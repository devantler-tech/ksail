package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

// ErrReplayStreamNil is returned when ReplayCapture is called with a nil pcap
// stream.
var ErrReplayStreamNil = errors.New("replay stream is nil")

// ErrReplayAddressEmpty is returned when ReplayCapture is called with an empty
// local address.
var ErrReplayAddressEmpty = errors.New("replay address is empty")

// ErrReplayAddressInvalid is returned when the replay address is not a valid
// host:port pair.
var ErrReplayAddressInvalid = errors.New("replay address must be host:port")

// ErrReplayUnreachable is returned when the replay address refuses the
// preflight connection, so a doomed session fails fast instead of capturing
// into the void.
var ErrReplayUnreachable = errors.New("replay address is unreachable")

// seqHalfRange is half the 32-bit TCP sequence space; a smaller-than-half
// forward distance marks a sequence number as "ahead" under RFC 1982 serial
// arithmetic, which keeps comparisons correct across wraparound.
const seqHalfRange = 1 << 31

// maxPendingSegments bounds the out-of-order buffer per flow; segments beyond
// it are dropped rather than growing memory without limit on a hole that
// never fills (mirror mode is observational, not lossless).
const maxPendingSegments = 256

// ReplayCapture parses a live pcap stream (as produced by the tap's tcpdump)
// and replays the inbound TCP payloads addressed to the captured service port
// to localAddr — one local connection per mirrored client flow, delivered in
// sequence order while the capture is still running. Responses from the local
// process are read and discarded: replay is strictly one-way, nothing flows
// back into the cluster (the reverse tunnel is Phase 2 of ksail#4521 by
// design). The call returns when the stream ends (EOF), when ctx is cancelled
// (clean stop, nil), or on a malformed stream or unreachable local address.
func ReplayCapture(
	ctx context.Context,
	pcapStream io.Reader,
	port int,
	localAddr string,
) error {
	err := validateReplayInputs(pcapStream, port, localAddr)
	if err != nil {
		return err
	}

	err = preflightReplayDial(ctx, localAddr)
	if err != nil {
		// A cancelled session is a clean stop, mirroring streamCapture's
		// convention — not an unreachable-address failure.
		if ctx.Err() != nil {
			return nil
		}

		return err
	}

	pcapReader, err := pcapgo.NewReader(pcapStream)
	if err != nil {
		return fmt.Errorf("read pcap header: %w", err)
	}

	replayer := newTCPReplayer(port, localAddr)
	defer replayer.closeAll()

	return replayer.run(ctx, pcapReader)
}

// validateReplayInputs rejects the nil/empty/out-of-range inputs before any
// network or stream work happens.
func validateReplayInputs(pcapStream io.Reader, port int, localAddr string) error {
	if pcapStream == nil {
		return ErrReplayStreamNil
	}

	if port < 1 || port > maxPort {
		return fmt.Errorf("%w: %d", ErrInvalidCapturePort, port)
	}

	if localAddr == "" {
		return ErrReplayAddressEmpty
	}

	_, _, err := net.SplitHostPort(localAddr)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrReplayAddressInvalid, localAddr)
	}

	return nil
}

// preflightReplayDial verifies the local destination accepts connections so an
// unreachable --to fails the session immediately instead of after a long
// capture.
func preflightReplayDial(ctx context.Context, localAddr string) error {
	var dialer net.Dialer

	conn, err := dialer.DialContext(ctx, "tcp", localAddr)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrReplayUnreachable, localAddr, err)
	}

	_ = conn.Close()

	return nil
}

// tcpReplayer reassembles the capture's inbound TCP flows and replays their
// payload bytes, in sequence order, to the configured local address.
type tcpReplayer struct {
	port      uint16
	localAddr string
	dialer    net.Dialer
	flows     map[string]*replayFlow
}

// newTCPReplayer builds a replayer for the given (pre-validated) service port
// and local destination.
func newTCPReplayer(port int, localAddr string) *tcpReplayer {
	return &tcpReplayer{
		port:      uint16(port), //nolint:gosec // G115: bounds-checked by validateReplayInputs.
		localAddr: localAddr,
		dialer:    net.Dialer{},
		flows:     map[string]*replayFlow{},
	}
}

// run consumes packet records until the stream ends or ctx is cancelled.
func (r *tcpReplayer) run(ctx context.Context, pcapReader *pcapgo.Reader) error {
	linkType := pcapReader.LinkType()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		data, _, err := pcapReader.ReadPacketData()
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read pcap packet: %w", err)
		}

		r.handlePacket(ctx, data, linkType)
	}
}

// handlePacket decodes one captured packet and feeds inbound TCP segments for
// the mirrored port into their flow. Anything that is not such a segment —
// other protocols, other ports, the workload's own responses — is skipped.
func (r *tcpReplayer) handlePacket(ctx context.Context, data []byte, linkType layers.LinkType) {
	packet := gopacket.NewPacket(data, linkType, gopacket.NoCopy)

	tcpLayer, ok := packet.Layer(layers.LayerTypeTCP).(*layers.TCP)
	if !ok || packet.NetworkLayer() == nil {
		return
	}

	if uint16(tcpLayer.DstPort) != r.port {
		return
	}

	key := packet.NetworkLayer().NetworkFlow().String() + "|" + tcpLayer.TransportFlow().String()

	flow := r.flowFor(ctx, key, tcpLayer)
	if flow == nil {
		return
	}

	flow.accept(tcpLayer)
}

// flowFor returns the replay flow for a segment's client connection, creating
// it — and dialing its local connection — when the segment starts (SYN) or
// mid-stream-joins a connection not seen before. Stray empty segments of
// unknown flows (late ACKs/FINs) create nothing.
func (r *tcpReplayer) flowFor(ctx context.Context, key string, tcpLayer *layers.TCP) *replayFlow {
	flow, exists := r.flows[key]
	if exists {
		return flow
	}

	if !tcpLayer.SYN && len(tcpLayer.Payload) == 0 {
		return nil
	}

	flow = newReplayFlow(tcpLayer)

	conn, err := r.dialer.DialContext(ctx, "tcp", r.localAddr)
	if err != nil {
		// The preflight passed, so a per-flow dial failure is local churn;
		// the flow is remembered as failed so retransmissions don't redial.
		flow.failed = true
	} else {
		flow.conn = conn

		go discardReplayResponses(conn)
	}

	r.flows[key] = flow

	return flow
}

// closeAll closes every open local connection at the end of the session.
func (r *tcpReplayer) closeAll() {
	for _, flow := range r.flows {
		flow.close()
	}
}

// discardReplayResponses drains whatever the local process writes back on a
// replayed connection; replay is one-way, so responses go nowhere.
func discardReplayResponses(conn net.Conn) {
	_, _ = io.Copy(io.Discard, conn)
}

// replayFlow tracks one mirrored client connection: its local replay
// connection, the next expected sequence number, and an out-of-order buffer.
type replayFlow struct {
	conn    net.Conn
	nextSeq uint32
	pending map[uint32][]byte
	failed  bool
}

// newReplayFlow seeds a flow's sequence cursor from its first segment: one
// past the SYN's sequence number for a connection observed from the start, or
// the segment's own sequence number when joining an established connection
// mid-capture.
func newReplayFlow(tcpLayer *layers.TCP) *replayFlow {
	nextSeq := tcpLayer.Seq
	if tcpLayer.SYN {
		nextSeq++
	}

	return &replayFlow{
		conn:    nil,
		nextSeq: nextSeq,
		pending: map[uint32][]byte{},
		failed:  false,
	}
}

// accept feeds one inbound segment into the flow: payload bytes are delivered
// in sequence order (buffering out-of-order segments, trimming or dropping
// retransmissions), and RST/FIN close the local connection.
func (f *replayFlow) accept(tcpLayer *layers.TCP) {
	if f.failed {
		return
	}

	if len(tcpLayer.Payload) > 0 && !tcpLayer.SYN {
		f.acceptPayload(tcpLayer.Seq, tcpLayer.Payload)
	}

	if tcpLayer.RST || tcpLayer.FIN {
		f.close()

		f.failed = true
	}
}

// acceptPayload orders one segment's payload against the flow's sequence
// cursor: in-order data is delivered (then any buffered successors), data from
// the past is trimmed to its unseen suffix or dropped as a retransmission, and
// future data is buffered until the hole before it fills.
func (f *replayFlow) acceptPayload(seq uint32, payload []byte) {
	if seq == f.nextSeq {
		f.deliver(payload)
		f.flushPending()

		return
	}

	behind := f.nextSeq - seq
	if behind < seqHalfRange {
		if int64(len(payload)) > int64(behind) {
			f.deliver(payload[behind:])
			f.flushPending()
		}

		return
	}

	if len(f.pending) < maxPendingSegments {
		f.pending[seq] = append([]byte(nil), payload...)
	}
}

// flushPending delivers buffered segments that have become in-order after the
// hole before them filled.
func (f *replayFlow) flushPending() {
	for {
		payload, ok := f.pending[f.nextSeq]
		if !ok {
			return
		}

		delete(f.pending, f.nextSeq)
		f.deliver(payload)
	}
}

// deliver writes in-order payload bytes to the local connection and advances
// the sequence cursor. A write failure (the local process hung up) marks the
// flow failed; the capture itself continues untouched.
func (f *replayFlow) deliver(payload []byte) {
	f.nextSeq += uint32(len(payload)) //nolint:gosec // G115: TCP segment payloads fit uint32.

	if f.conn == nil || f.failed {
		return
	}

	_, err := f.conn.Write(payload)
	if err != nil {
		f.close()

		f.failed = true
	}
}

// close closes the flow's local connection, if it has one.
func (f *replayFlow) close() {
	if f.conn == nil {
		return
	}

	_ = f.conn.Close()
	f.conn = nil
}

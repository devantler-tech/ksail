package mirror

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

// ErrReplayAddressEmpty is returned when NewLiveReplay is called with an empty
// local address.
var ErrReplayAddressEmpty = errors.New("replay address is empty")

// ErrReplayPendingOverflow is recorded when a flow accumulates more
// out-of-order segments than maxPendingSegments — the overflowing segment is
// dropped so a pathological capture cannot grow memory without bound.
var ErrReplayPendingOverflow = errors.New(
	"replay flow dropped an out-of-order segment (pending buffer full)",
)

// ErrReplayTruncatedCapture is returned by Close when the pcap stream ended
// in the middle of a packet record — the capture was cut off, so the tail of
// the mirrored traffic may be missing.
var ErrReplayTruncatedCapture = errors.New(
	"pcap stream ended mid-packet (truncated capture)",
)

// maxPendingSegments bounds how many out-of-order TCP segments a single flow
// buffers while waiting for the gap to fill.
const maxPendingSegments = 256

// defaultReplayWriteTimeout bounds each write to the local process so a
// stalled local reader cannot block the parser goroutine — and through the
// pipe, the capture itself — indefinitely.
const defaultReplayWriteTimeout = 5 * time.Second

// ReplayDialer opens the local connection a mirrored flow is replayed into.
// It is a seam so tests can capture the dialed connections.
type ReplayDialer func(network, address string) (net.Conn, error)

// ReplayOption customises NewLiveReplay.
type ReplayOption func(*replayConfig)

type replayConfig struct {
	dial         ReplayDialer
	writeTimeout time.Duration
}

// WithReplayDialer overrides how the replay dials the local address.
// Production code leaves the net.Dial default; tests use it to observe
// connections without a real listener.
func WithReplayDialer(dial ReplayDialer) ReplayOption {
	return func(cfg *replayConfig) {
		if dial != nil {
			cfg.dial = dial
		}
	}
}

// WithReplayWriteTimeout overrides the per-write deadline on local
// connections. Production code leaves the default; tests use a short timeout
// to exercise the stalled-local-reader path quickly.
func WithReplayWriteTimeout(timeout time.Duration) ReplayOption {
	return func(cfg *replayConfig) {
		if timeout > 0 {
			cfg.writeTimeout = timeout
		}
	}
}

// LiveReplay parses a pcap capture stream as it arrives and replays the
// inbound TCP payload streams (segments addressed TO the mirrored service
// port) to a local address — one local connection per mirrored flow, live,
// while the capture is still running. It implements io.Writer so the capture
// session can tee into it alongside the file/stdout destination.
//
// Read-only stays read-only: whatever the local process answers is read and
// discarded; nothing flows back into the cluster (the reverse tunnel is a
// later phase by design). Delivery is per-flow in-order via a minimal
// sequencer — out-of-order segments are buffered until the gap fills,
// retransmissions are dropped, overlaps are trimmed — chosen over
// gopacket/reassembly as the smallest correct option for one-directional
// payload extraction.
//
// A malformed pcap stream stops the parser and fails subsequent Writes, which
// ends the capture session with an error (a corrupt stream means the tap
// broke). A failure on the LOCAL side — dial refused, write reset — never
// stops the capture: the affected flow is dropped, the first such error is
// remembered, and Close returns it so the command can surface what went wrong.
type LiveReplay struct {
	address      string
	port         int
	dial         ReplayDialer
	writeTimeout time.Duration

	writer *io.PipeWriter
	done   chan struct{}

	mu       sync.Mutex
	firstErr error
}

// NewLiveReplay validates the local address and starts the replay parser; the
// returned LiveReplay must be Closed after the capture session ends. The port
// is the mirrored service port — only TCP segments addressed to it are
// replayed (the capture also carries the pod's responses, which must not be
// echoed into the local process).
func NewLiveReplay(address string, port int, opts ...ReplayOption) (*LiveReplay, error) {
	if address == "" {
		return nil, ErrReplayAddressEmpty
	}

	_, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse replay address %q: %w", address, err)
	}

	if port < 1 || port > maxPort {
		return nil, fmt.Errorf("%w: %d", ErrInvalidCapturePort, port)
	}

	cfg := replayConfig{dial: net.Dial, writeTimeout: defaultReplayWriteTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}

	reader, writer := io.Pipe()
	replay := &LiveReplay{
		address:      address,
		port:         port,
		dial:         cfg.dial,
		writeTimeout: cfg.writeTimeout,
		writer:       writer,
		done:         make(chan struct{}),
		mu:           sync.Mutex{},
		firstErr:     nil,
	}

	go replay.run(reader)

	return replay, nil
}

// Write feeds capture-stream bytes to the replay parser. It blocks only as
// long as the parser needs to consume them and fails once the parser has
// stopped on a malformed stream.
func (r *LiveReplay) Write(data []byte) (int, error) {
	count, err := r.writer.Write(data)
	if err != nil {
		return count, fmt.Errorf("write to replay parser: %w", err)
	}

	return count, nil
}

// Close ends the capture stream, waits for the parser to drain and close
// every local connection, and returns the first local-side replay error (nil
// when every mirrored flow was delivered cleanly).
func (r *LiveReplay) Close() error {
	_ = r.writer.Close()
	<-r.done

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.firstErr
}

// recordErr remembers the first local-side error; later ones add no signal.
func (r *LiveReplay) recordErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.firstErr == nil {
		r.firstErr = err
	}
}

// run is the parser goroutine: it reads the pcap stream, hands every packet
// to the sequencer, and on return closes every local connection. A pcap-level
// error is propagated to the pipe so the capture session's next Write fails.
func (r *LiveReplay) run(reader *io.PipeReader) {
	defer close(r.done)

	flows := make(map[string]*replayFlow)

	defer func() {
		for _, flow := range flows {
			flow.close()
		}
	}()

	pcapReader, err := pcapgo.NewReader(reader)
	if err != nil {
		_ = reader.CloseWithError(fmt.Errorf("read pcap header: %w", err))

		return
	}

	linkType := pcapReader.LinkType()

	for {
		data, _, err := pcapReader.ReadPacketData()
		if errors.Is(err, io.EOF) {
			return
		}

		// A stream ending mid-packet is a truncated capture, not a clean
		// shutdown: remember it so Close surfaces it (the pipe is already
		// closed at this point, so failing the pipe alone would be silent).
		if errors.Is(err, io.ErrUnexpectedEOF) {
			r.recordErr(ErrReplayTruncatedCapture)
			_ = reader.CloseWithError(ErrReplayTruncatedCapture)

			return
		}

		if err != nil {
			_ = reader.CloseWithError(fmt.Errorf("read pcap packet: %w", err))

			return
		}

		r.handlePacket(flows, linkType, data)
	}
}

// handlePacket decodes one captured packet and, when it is an inbound TCP
// segment for the mirrored port, feeds it to its flow's sequencer.
func (r *LiveReplay) handlePacket(
	flows map[string]*replayFlow,
	linkType layers.LinkType,
	data []byte,
) {
	packet := gopacket.NewPacket(data, linkType, gopacket.Lazy)

	tcpLayer, isTCP := packet.Layer(layers.LayerTypeTCP).(*layers.TCP)
	if !isTCP {
		return
	}

	networkLayer := packet.NetworkLayer()
	if networkLayer == nil {
		return
	}

	// Only the inbound direction is replayed: segments addressed TO the
	// mirrored service port. The pod's responses (source port == service
	// port) stay in the pcap but never reach the local process.
	if int(tcpLayer.DstPort) != r.port {
		return
	}

	key := networkLayer.NetworkFlow().String() + "|" + tcpLayer.TransportFlow().String()

	flow, exists := flows[key]
	if !exists {
		flow = r.newFlow()
		flows[key] = flow
	}

	flow.handleSegment(tcpLayer)

	if flow.finished {
		flow.close()
		delete(flows, key)
	}
}

// newFlow dials one local connection for a newly-seen mirrored flow. A dial
// failure is recorded and leaves the flow connection-less: its bytes are
// dropped while the capture (and every other flow) continues.
func (r *LiveReplay) newFlow() *replayFlow {
	flow := &replayFlow{
		conn:         nil,
		record:       r.recordErr,
		writeTimeout: r.writeTimeout,
		next:         0,
		initialized:  false,
		pending:      make(map[uint32][]byte),
		finSeen:      false,
		finPoint:     0,
		finished:     false,
	}

	conn, err := r.dial(protocolTCP, r.address)
	if err != nil {
		r.recordErr(fmt.Errorf("dial replay address %q: %w", r.address, err))

		return flow
	}

	// Read-only bridge: drain and discard whatever the local process answers.
	go func() {
		_, _ = io.Copy(io.Discard, conn)
	}()

	flow.conn = conn

	return flow
}

// replayFlow sequences one mirrored TCP flow's inbound payload onto one local
// connection: in-order delivery, bounded buffering of out-of-order segments,
// retransmissions dropped, overlaps trimmed.
type replayFlow struct {
	conn         net.Conn
	record       func(error)
	writeTimeout time.Duration

	next        uint32
	initialized bool
	pending     map[uint32][]byte
	finSeen     bool
	finPoint    uint32
	finished    bool
}

// handleSegment advances the sequencer with one TCP segment.
func (f *replayFlow) handleSegment(segment *layers.TCP) {
	sequence := segment.Seq

	if segment.SYN {
		// The SYN consumes one sequence number; payload (TCP Fast Open)
		// occupies sequence space from seq+1.
		f.next = sequence + 1
		f.initialized = true
		sequence++
	} else if !f.initialized {
		// Mid-stream attach (the tap started after the flow): accept from
		// the first segment seen.
		f.next = sequence
		f.initialized = true
	}

	if len(segment.Payload) > 0 {
		f.consume(sequence, segment.Payload)
	}

	// A FIN consumes sequence space AFTER its payload, so an out-of-order
	// FIN must not close the flow while earlier segments are still pending:
	// remember its close point and finish only once delivery reaches it.
	// An RST is an immediate abort — pending data is gone by definition.
	if segment.FIN && !f.finSeen {
		f.finSeen = true
		//nolint:gosec // G115: a TCP segment is far below 2^32 bytes.
		f.finPoint = sequence + uint32(len(segment.Payload))
	}

	//nolint:gosec // G115: two's-complement wraparound distance is intentional.
	if segment.RST || (f.finSeen && int32(f.finPoint-f.next) <= 0) {
		f.finished = true
	}
}

// consume delivers a payload segment in sequence order, buffering ahead-of-
// order data and trimming retransmitted overlap.
func (f *replayFlow) consume(sequence uint32, payload []byte) {
	// Signed distance handles sequence-number wraparound.
	//nolint:gosec // G115: two's-complement wraparound distance is intentional.
	distance := int32(sequence - f.next)

	switch {
	case distance == 0:
		f.deliver(payload)
	case distance > 0:
		existing, exists := f.pending[sequence]
		if exists && len(existing) >= len(payload) {
			// A shorter duplicate must not replace a longer buffered
			// segment — that would silently drop the longer one's tail.
			break
		}

		if !exists && len(f.pending) >= maxPendingSegments {
			f.record(ErrReplayPendingOverflow)

			return
		}

		buffered := make([]byte, len(payload))
		copy(buffered, payload)
		f.pending[sequence] = buffered
	default:
		// Overlap: deliver only the bytes past what was already replayed.
		overlap := -distance
		if int(overlap) < len(payload) {
			f.deliver(payload[overlap:])
		}
	}

	f.flushPending()
}

// flushPending delivers buffered segments that have become deliverable: an
// exact continuation of the replayed prefix, or one that now overlaps it
// (only the still-new tail is delivered). Delivery advances f.next, which can
// make further buffered segments deliverable, so it loops until a pass makes
// no progress.
func (f *replayFlow) flushPending() {
	for {
		progressed := false

		for start, payload := range f.pending {
			//nolint:gosec // G115: two's-complement wraparound distance is intentional.
			distance := int32(start - f.next)
			if distance > 0 {
				continue
			}

			delete(f.pending, start)

			overlap := -distance
			if int(overlap) < len(payload) {
				f.deliver(payload[overlap:])

				progressed = true
			}
		}

		if !progressed {
			return
		}
	}
}

// deliver writes in-order payload to the local connection and advances the
// expected sequence number. Each write carries a deadline so a local process
// that accepts but stops reading cannot stall the parser (and, through the
// pipe, the capture). A local write failure or timeout drops the connection
// (the flow's remaining bytes are discarded) but never stops the capture.
func (f *replayFlow) deliver(payload []byte) {
	f.next += uint32(len(payload)) //nolint:gosec // G115: a TCP segment is far below 2^32 bytes.

	if f.conn == nil {
		return
	}

	_ = f.conn.SetWriteDeadline(time.Now().Add(f.writeTimeout))

	_, err := f.conn.Write(payload)
	if err != nil {
		f.record(fmt.Errorf("replay to local address: %w", err))
		_ = f.conn.Close()
		f.conn = nil
	}
}

// close closes the flow's local connection, if it still has one.
func (f *replayFlow) close() {
	if f.conn != nil {
		_ = f.conn.Close()
		f.conn = nil
	}
}

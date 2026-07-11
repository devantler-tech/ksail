package mirror

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// FrameType identifies what a tunnel frame carries: the lifecycle of one
// multiplexed connection (Open/Close) or a chunk of its bytes (Data).
type FrameType uint8

const (
	// FrameOpen announces a new multiplexed connection on a StreamID. It
	// carries no payload.
	FrameOpen FrameType = 1
	// FrameData carries a chunk of bytes for an already-open StreamID.
	FrameData FrameType = 2
	// FrameClose ends a multiplexed connection on a StreamID. It carries no
	// payload; any buffered Data for the stream was sent in earlier frames.
	FrameClose FrameType = 3
	// FrameKeepalive is a session-level liveness ping. The intercept client
	// sends one periodically so the steering agent can tell a live-but-idle
	// client from a dead one (the exec stream does not reliably deliver EOF
	// when the client dies — ksail#6040). It belongs to no stream (StreamID
	// 0, which the odd/even allocation never assigns) and carries no
	// payload; receivers refresh their liveness deadline and otherwise
	// ignore it.
	FrameKeepalive FrameType = 4
)

// tunnelHeaderSize is the fixed on-wire header: StreamID (uint32) + Type
// (uint8) + Length (uint32), big-endian.
const tunnelHeaderSize = 4 + 1 + 4

// MaxTunnelPayload bounds a single FrameData payload. A frame declaring a
// larger length is rejected before any allocation, so a malformed or hostile
// stream cannot make the reader allocate unbounded memory — the same
// defensive stance the replay parser takes with maxPendingSegments. 64 KiB
// matches a comfortable chunk for the exec transport without over-fragmenting.
const MaxTunnelPayload = 64 * 1024

// ErrTunnelFrameTooLarge is returned when a frame's declared payload length
// exceeds MaxTunnelPayload.
var ErrTunnelFrameTooLarge = errors.New("tunnel frame payload exceeds the maximum size")

// ErrTunnelUnknownFrameType is returned when a frame carries a type that is
// not one of FrameOpen/FrameData/FrameClose.
var ErrTunnelUnknownFrameType = errors.New("tunnel frame has an unknown type")

// ErrTunnelControlFramePayload is returned when a control frame (FrameOpen or
// FrameClose) carries a non-empty payload — control frames must be empty so a
// peer never has to guess whether their bytes are meaningful.
var ErrTunnelControlFramePayload = errors.New("tunnel control frame must not carry a payload")

// Frame is one unit on the multiplexing tunnel that will carry intercepted
// connections back and forth over the single bidirectional exec byte stream
// (SPDY stdin+stdout). Each frame belongs to a StreamID — one intercepted
// connection — so many connections share the one channel. It is the wire
// format the Phase 2 intercept mux/demux is built on; see [doc.go] and
// ksail#4521.
type Frame struct {
	// StreamID identifies the multiplexed connection this frame belongs to.
	StreamID uint32
	// Type is the frame's role in that connection's lifecycle.
	Type FrameType
	// Payload is the byte chunk for a FrameData frame; it must be empty for
	// FrameOpen and FrameClose.
	Payload []byte
}

// checkFrameLength validates a frame type against its declared payload length.
// It is shared by the writer (before encoding) and the reader (after decoding
// the header, before allocating the payload), so both sides enforce the same
// invariants: only FrameData may carry bytes, FrameData is capped at
// MaxTunnelPayload, and control frames must be empty.
func checkFrameLength(frameType FrameType, length uint32) error {
	switch frameType {
	case FrameData:
		if length > MaxTunnelPayload {
			return fmt.Errorf("%w: %d bytes", ErrTunnelFrameTooLarge, length)
		}
	case FrameOpen, FrameClose, FrameKeepalive:
		if length > 0 {
			return fmt.Errorf(
				"%w: type %d has %d bytes",
				ErrTunnelControlFramePayload,
				frameType,
				length,
			)
		}
	default:
		return fmt.Errorf("%w: %d", ErrTunnelUnknownFrameType, frameType)
	}

	return nil
}

// WriteFrame encodes frame to writer as a length-prefixed, self-framing record
// so a peer can read exactly one frame back with [ReadFrame] over a byte
// stream that carries no message boundaries of its own. A malformed frame
// (unknown type, oversized Data payload, or a control frame with a payload) is
// rejected before anything is written.
func WriteFrame(writer io.Writer, frame Frame) error {
	// Bound the payload on the un-narrowed int first: narrowing an oversized
	// length to uint32 could wrap into range and desync header from payload.
	if len(frame.Payload) > MaxTunnelPayload {
		return fmt.Errorf("%w: %d bytes", ErrTunnelFrameTooLarge, len(frame.Payload))
	}

	length := uint32(len(frame.Payload)) //nolint:gosec // G115: bounded above.

	err := checkFrameLength(frame.Type, length)
	if err != nil {
		return err
	}

	var header [tunnelHeaderSize]byte
	binary.BigEndian.PutUint32(header[0:4], frame.StreamID)
	header[4] = byte(frame.Type)
	binary.BigEndian.PutUint32(header[5:9], length)

	_, err = writer.Write(header[:])
	if err != nil {
		return fmt.Errorf("writing tunnel frame header: %w", err)
	}

	if length > 0 {
		_, err = writer.Write(frame.Payload)
		if err != nil {
			return fmt.Errorf("writing tunnel frame payload: %w", err)
		}
	}

	return nil
}

// ReadFrame decodes exactly one frame from reader. A clean end of stream at a
// frame boundary is reported as io.EOF; a stream that ends part-way through a
// header or payload is reported as io.ErrUnexpectedEOF (the tunnel was cut off
// mid-frame). A frame whose header declares an oversized payload or an unknown
// type is rejected without reading the payload, so a hostile length can never
// drive an allocation.
func ReadFrame(reader io.Reader) (Frame, error) {
	var header [tunnelHeaderSize]byte

	_, err := io.ReadFull(reader, header[:])
	if err != nil {
		// io.ReadFull maps a clean boundary EOF to io.EOF and a partial read
		// to io.ErrUnexpectedEOF; pass the sentinel through so callers can
		// distinguish a graceful close from a truncated tunnel.
		if errors.Is(err, io.EOF) {
			return Frame{}, io.EOF
		}

		return Frame{}, fmt.Errorf("reading tunnel frame header: %w", err)
	}

	frame := Frame{
		StreamID: binary.BigEndian.Uint32(header[0:4]),
		Type:     FrameType(header[4]),
		Payload:  nil,
	}
	length := binary.BigEndian.Uint32(header[5:9])

	err = checkFrameLength(frame.Type, length)
	if err != nil {
		return Frame{}, err
	}

	if length > 0 {
		frame.Payload = make([]byte, length)

		_, err = io.ReadFull(reader, frame.Payload)
		if err != nil {
			// Any EOF here is mid-frame — the header promised bytes that never
			// arrived — so report it as a truncation, not a clean close.
			if errors.Is(err, io.EOF) {
				return Frame{}, io.ErrUnexpectedEOF
			}

			return Frame{}, fmt.Errorf("reading tunnel frame payload: %w", err)
		}
	}

	return frame, nil
}

package mirror_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadFrame_RoundTrip(t *testing.T) {
	t.Parallel()

	frames := []mirror.Frame{
		{StreamID: 1, Type: mirror.FrameOpen},
		{StreamID: 1, Type: mirror.FrameData, Payload: []byte("GET / HTTP/1.1\r\n\r\n")},
		{StreamID: 1, Type: mirror.FrameData, Payload: []byte{}},
		{StreamID: 4294967295, Type: mirror.FrameClose},
	}

	for _, frame := range frames {
		var buf bytes.Buffer

		require.NoError(t, mirror.WriteFrame(&buf, frame))

		got, err := mirror.ReadFrame(&buf)
		require.NoError(t, err)
		assert.Equal(t, frame.StreamID, got.StreamID)
		assert.Equal(t, frame.Type, got.Type)
		// An empty payload round-trips to nil (ReadFrame allocates only for a
		// non-zero length); compare content so nil and empty count as equal.
		assert.Equal(t, string(frame.Payload), string(got.Payload))
	}
}

func TestReadFrame_DecodesInterleavedStreams(t *testing.T) {
	t.Parallel()

	// Two multiplexed connections interleave their frames on the one stream;
	// each frame must decode back to its own StreamID in order.
	written := []mirror.Frame{
		{StreamID: 1, Type: mirror.FrameOpen},
		{StreamID: 2, Type: mirror.FrameOpen},
		{StreamID: 1, Type: mirror.FrameData, Payload: []byte("one-a")},
		{StreamID: 2, Type: mirror.FrameData, Payload: []byte("two-a")},
		{StreamID: 1, Type: mirror.FrameData, Payload: []byte("one-b")},
		{StreamID: 1, Type: mirror.FrameClose},
		{StreamID: 2, Type: mirror.FrameClose},
	}

	var buf bytes.Buffer
	for _, frame := range written {
		require.NoError(t, mirror.WriteFrame(&buf, frame))
	}

	for _, want := range written {
		got, err := mirror.ReadFrame(&buf)
		require.NoError(t, err)
		assert.Equal(t, want.StreamID, got.StreamID)
		assert.Equal(t, want.Type, got.Type)
		assert.Equal(t, string(want.Payload), string(got.Payload))
	}

	// The buffer is exhausted at a clean frame boundary.
	_, err := mirror.ReadFrame(&buf)
	require.ErrorIs(t, err, io.EOF)
}

func TestReadFrame_EmptyStreamReturnsEOF(t *testing.T) {
	t.Parallel()

	_, err := mirror.ReadFrame(bytes.NewReader(nil))

	require.ErrorIs(t, err, io.EOF)
}

func TestReadFrame_TruncatedHeaderIsUnexpectedEOF(t *testing.T) {
	t.Parallel()

	var full bytes.Buffer
	require.NoError(t, mirror.WriteFrame(&full, mirror.Frame{StreamID: 7, Type: mirror.FrameOpen}))

	// Cut the encoded frame short inside its 9-byte header.
	truncated := full.Bytes()[:4]

	_, err := mirror.ReadFrame(bytes.NewReader(truncated))
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestReadFrame_TruncatedPayloadIsUnexpectedEOF(t *testing.T) {
	t.Parallel()

	var full bytes.Buffer
	require.NoError(t, mirror.WriteFrame(&full, mirror.Frame{
		StreamID: 3,
		Type:     mirror.FrameData,
		Payload:  []byte("the full payload"),
	}))

	// Keep the whole header but drop the last few payload bytes.
	encoded := full.Bytes()
	truncated := encoded[:len(encoded)-3]

	_, err := mirror.ReadFrame(bytes.NewReader(truncated))
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestWriteFrame_RejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := mirror.WriteFrame(&buf, mirror.Frame{
		StreamID: 1,
		Type:     mirror.FrameData,
		Payload:  make([]byte, mirror.MaxTunnelPayload+1),
	})

	require.ErrorIs(t, err, mirror.ErrTunnelFrameTooLarge)
	assert.Zero(t, buf.Len(), "no bytes should be written when the frame is rejected")
}

func TestWriteFrame_AcceptsMaxPayload(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte{0xAB}, mirror.MaxTunnelPayload)

	var buf bytes.Buffer
	require.NoError(t, mirror.WriteFrame(&buf, mirror.Frame{
		StreamID: 1,
		Type:     mirror.FrameData,
		Payload:  payload,
	}))

	got, err := mirror.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, payload, got.Payload)
}

func TestReadFrame_RejectsOversizedDeclaredLength(t *testing.T) {
	t.Parallel()

	// Hand-craft a header that declares more than MaxTunnelPayload without
	// ever supplying the bytes — the reader must reject it before allocating.
	header := make([]byte, 9)
	header[4] = byte(mirror.FrameData)
	// Length field (bytes 5..9), big-endian, one over the cap.
	binary.BigEndian.PutUint32(header[5:9], uint32(mirror.MaxTunnelPayload+1))

	_, err := mirror.ReadFrame(bytes.NewReader(header))
	require.ErrorIs(t, err, mirror.ErrTunnelFrameTooLarge)
}

func TestWriteFrame_RejectsUnknownType(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := mirror.WriteFrame(&buf, mirror.Frame{StreamID: 1, Type: mirror.FrameType(99)})

	require.ErrorIs(t, err, mirror.ErrTunnelUnknownFrameType)
	assert.Zero(t, buf.Len())
}

func TestReadFrame_RejectsUnknownType(t *testing.T) {
	t.Parallel()

	header := make([]byte, 9)
	header[4] = 99 // unknown type, zero length

	_, err := mirror.ReadFrame(bytes.NewReader(header))
	require.ErrorIs(t, err, mirror.ErrTunnelUnknownFrameType)
}

func TestWriteFrame_RejectsPayloadOnControlFrame(t *testing.T) {
	t.Parallel()

	for _, frameType := range []mirror.FrameType{mirror.FrameOpen, mirror.FrameClose} {
		var buf bytes.Buffer

		err := mirror.WriteFrame(&buf, mirror.Frame{
			StreamID: 1,
			Type:     frameType,
			Payload:  []byte("unexpected"),
		})

		require.ErrorIs(t, err, mirror.ErrTunnelControlFramePayload)
		assert.Zero(t, buf.Len())
	}
}

func TestReadFrame_RejectsPayloadOnControlFrame(t *testing.T) {
	t.Parallel()

	// A control frame header that dishonestly declares a payload length.
	header := make([]byte, 9)
	header[4] = byte(mirror.FrameOpen)
	header[8] = 5 // length = 5

	_, err := mirror.ReadFrame(bytes.NewReader(header))
	require.ErrorIs(t, err, mirror.ErrTunnelControlFramePayload)
}

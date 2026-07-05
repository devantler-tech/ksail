package mirror_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTimeout bounds every blocking wait in these tests so a mux bug fails
// fast instead of hanging the suite.
const testTimeout = 5 * time.Second

// errUnexpectedRequest reports a server-side payload mismatch from a test
// goroutine, where assertions must not run.
var errUnexpectedRequest = errors.New("unexpected request payload")

// errTransportTorn stands in for a transport failure under the session.
var errTransportTorn = errors.New("transport torn")

// newSessionPair joins a client and a server session with in-memory pipes,
// exactly as the exec stream will join them in Phase 2.
func newSessionPair(t *testing.T) (*mirror.TunnelSession, *mirror.TunnelSession) {
	t.Helper()

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	client := mirror.NewTunnelSession(clientReader, clientWriter, mirror.TunnelRoleClient)
	server := mirror.NewTunnelSession(serverReader, serverWriter, mirror.TunnelRoleServer)

	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	return client, server
}

// acceptContext returns a context that bounds an AcceptStream wait.
func acceptContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)

	return ctx
}

func TestTunnelSession_RoundTrip(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	ctx := acceptContext(t)
	serverDone := make(chan error, 1)

	go func() {
		accepted, err := server.AcceptStream(ctx)
		if err != nil {
			serverDone <- err

			return
		}

		request := make([]byte, len("hello"))

		_, err = io.ReadFull(accepted, request)
		if err != nil {
			serverDone <- err

			return
		}

		if string(request) != "hello" {
			serverDone <- fmt.Errorf("%w: %q", errUnexpectedRequest, request)

			return
		}

		_, err = accepted.Write([]byte("world"))
		serverDone <- err
	}()

	stream, err := client.OpenStream()
	require.NoError(t, err)

	_, err = stream.Write([]byte("hello"))
	require.NoError(t, err)

	response := make([]byte, len("world"))

	_, err = io.ReadFull(stream, response)
	require.NoError(t, err)
	assert.Equal(t, "world", string(response))

	require.NoError(t, <-serverDone)
}

func TestTunnelSession_StreamIDParityNeverCollides(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	clientFirst, err := client.OpenStream()
	require.NoError(t, err)

	clientSecond, err := client.OpenStream()
	require.NoError(t, err)

	serverFirst, err := server.OpenStream()
	require.NoError(t, err)

	assert.Equal(t, uint32(1), clientFirst.StreamID())
	assert.Equal(t, uint32(3), clientSecond.StreamID())
	assert.Equal(t, uint32(2), serverFirst.StreamID())
}

func TestTunnelSession_ConcurrentStreamsDoNotCrossTalk(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	const streamCount = 4

	ctx := acceptContext(t)

	// The server echoes every accepted stream back until the peer closes it.
	for range streamCount {
		go func() {
			accepted, err := server.AcceptStream(ctx)
			if err != nil {
				return
			}

			_, _ = io.Copy(accepted, accepted)
			_ = accepted.Close()
		}()
	}

	var waitGroup sync.WaitGroup

	results := make(chan error, streamCount)

	for index := range streamCount {
		waitGroup.Go(func() {
			results <- echoOnce(client, fmt.Sprintf("stream-%d-payload", index))
		})
	}

	waitGroup.Wait()
	close(results)

	for err := range results {
		require.NoError(t, err)
	}
}

// echoOnce opens a stream, sends message, and verifies the peer echoes it
// back verbatim before closing the stream. It runs in test goroutines, so it
// reports by error instead of asserting.
func echoOnce(session *mirror.TunnelSession, message string) error {
	stream, err := session.OpenStream()
	if err != nil {
		return fmt.Errorf("opening stream: %w", err)
	}

	_, err = stream.Write([]byte(message))
	if err != nil {
		return fmt.Errorf("writing message: %w", err)
	}

	echoed := make([]byte, len(message))

	_, err = io.ReadFull(stream, echoed)
	if err != nil {
		return fmt.Errorf("reading echo: %w", err)
	}

	if string(echoed) != message {
		return fmt.Errorf("%w: %q", errUnexpectedRequest, echoed)
	}

	err = stream.Close()
	if err != nil {
		return fmt.Errorf("closing stream: %w", err)
	}

	return nil
}

func TestTunnelStream_ChunksWritesLargerThanMaxPayload(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	payload := make([]byte, mirror.MaxTunnelPayload+mirror.MaxTunnelPayload/2)
	for index := range payload {
		payload[index] = byte(index % 251)
	}

	ctx := acceptContext(t)
	received := make(chan []byte, 1)
	serverErr := make(chan error, 1)

	go func() {
		accepted, err := server.AcceptStream(ctx)
		if err != nil {
			serverErr <- err

			return
		}

		buffer := make([]byte, len(payload))

		_, err = io.ReadFull(accepted, buffer)
		serverErr <- err

		received <- buffer
	}()

	stream, err := client.OpenStream()
	require.NoError(t, err)

	count, err := stream.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), count)

	require.NoError(t, <-serverErr)
	assert.Equal(t, payload, <-received)
}

func TestTunnelStream_CloseUnblocksPeerReadsWithEOF(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	stream, err := client.OpenStream()
	require.NoError(t, err)

	_, err = stream.Write([]byte("bye"))
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	accepted, err := server.AcceptStream(acceptContext(t))
	require.NoError(t, err)

	drained, err := io.ReadAll(accepted)
	require.NoError(t, err)
	assert.Equal(t, "bye", string(drained))
}

func TestTunnelStream_DoubleCloseIsSafe(t *testing.T) {
	t.Parallel()

	client, _ := newSessionPair(t)

	stream, err := client.OpenStream()
	require.NoError(t, err)

	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
}

func TestTunnelStream_WriteAfterCloseFails(t *testing.T) {
	t.Parallel()

	client, _ := newSessionPair(t)

	stream, err := client.OpenStream()
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	_, err = stream.Write([]byte("late"))
	require.ErrorIs(t, err, mirror.ErrTunnelStreamClosed)
}

func TestTunnelStream_ReadAfterLocalCloseFails(t *testing.T) {
	t.Parallel()

	client, _ := newSessionPair(t)

	stream, err := client.OpenStream()
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	_, err = stream.Read(make([]byte, 1))
	require.ErrorIs(t, err, mirror.ErrTunnelStreamClosed)
}

func TestTunnelSession_CloseUnblocksAcceptAndOpen(t *testing.T) {
	t.Parallel()

	client, _ := newSessionPair(t)

	require.NoError(t, client.Close())

	waitDone(t, client)

	_, err := client.AcceptStream(acceptContext(t))
	require.ErrorIs(t, err, mirror.ErrTunnelSessionClosed)

	_, err = client.OpenStream()
	require.ErrorIs(t, err, mirror.ErrTunnelSessionClosed)
}

func TestTunnelSession_CloseSurfacesOnOpenStreams(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	stream, err := client.OpenStream()
	require.NoError(t, err)

	require.NoError(t, client.Close())

	_, err = stream.Read(make([]byte, 1))
	require.ErrorIs(t, err, mirror.ErrTunnelSessionClosed)

	// The server's side of the tunnel lost its transport; it tears down too.
	waitDone(t, server)
}

func TestTunnelSession_AcceptHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	client, _ := newSessionPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.AcceptStream(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTunnelSession_DuplicateOpenTearsDownSession(t *testing.T) {
	t.Parallel()

	feedReader, feedWriter := io.Pipe()
	session := mirror.NewTunnelSession(feedReader, io.Discard, mirror.TunnelRoleServer)

	t.Cleanup(func() {
		_ = session.Close()
	})

	openFrame := mirror.Frame{StreamID: 1, Type: mirror.FrameOpen, Payload: nil}

	go func() {
		_ = mirror.WriteFrame(feedWriter, openFrame)
		_ = mirror.WriteFrame(feedWriter, openFrame)
	}()

	waitDone(t, session)
	require.ErrorIs(t, session.Err(), mirror.ErrTunnelDuplicateOpen)
}

func TestTunnelSession_DataForUnknownStreamIsDropped(t *testing.T) {
	t.Parallel()

	feedReader, feedWriter := io.Pipe()
	session := mirror.NewTunnelSession(feedReader, io.Discard, mirror.TunnelRoleServer)

	t.Cleanup(func() {
		_ = session.Close()
	})

	go func() {
		_ = mirror.WriteFrame(feedWriter, mirror.Frame{
			StreamID: 9,
			Type:     mirror.FrameData,
			Payload:  []byte("orphan"),
		})
		_ = mirror.WriteFrame(
			feedWriter,
			mirror.Frame{StreamID: 1, Type: mirror.FrameOpen, Payload: nil},
		)
	}()

	accepted, err := session.AcceptStream(acceptContext(t))
	require.NoError(t, err)
	assert.Equal(t, uint32(1), accepted.StreamID())
	require.NoError(t, session.Err())
}

func TestTunnelSession_PeerEOFTearsDownCleanly(t *testing.T) {
	t.Parallel()

	feedReader, feedWriter := io.Pipe()
	session := mirror.NewTunnelSession(feedReader, io.Discard, mirror.TunnelRoleServer)

	t.Cleanup(func() {
		_ = session.Close()
	})

	require.NoError(t, feedWriter.Close())

	waitDone(t, session)
	require.NoError(t, session.Err())
}

func TestTunnelSession_TransportErrorSurfacesOnErr(t *testing.T) {
	t.Parallel()

	feedReader, feedWriter := io.Pipe()
	session := mirror.NewTunnelSession(feedReader, io.Discard, mirror.TunnelRoleServer)

	t.Cleanup(func() {
		_ = session.Close()
	})

	require.NoError(t, feedWriter.CloseWithError(errTransportTorn))

	waitDone(t, session)
	require.ErrorIs(t, session.Err(), errTransportTorn)
}

// waitDone fails the test if the session's demux loop does not exit within
// the test timeout.
func waitDone(t *testing.T, session *mirror.TunnelSession) {
	t.Helper()

	select {
	case <-session.Done():
	case <-time.After(testTimeout):
		t.Fatal("tunnel session did not tear down in time")
	}
}

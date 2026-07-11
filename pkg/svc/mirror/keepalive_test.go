package mirror_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadFrame_KeepaliveRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(
		t,
		mirror.WriteFrame(&buf, mirror.Frame{StreamID: 0, Type: mirror.FrameKeepalive}),
	)

	got, err := mirror.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), got.StreamID)
	assert.Equal(t, mirror.FrameKeepalive, got.Type)
	assert.Empty(t, got.Payload)
}

func TestWriteFrame_RejectsKeepalivePayload(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := mirror.WriteFrame(&buf, mirror.Frame{
		StreamID: 0,
		Type:     mirror.FrameKeepalive,
		Payload:  []byte("no payload allowed"),
	})
	require.ErrorIs(t, err, mirror.ErrTunnelControlFramePayload)
}

func TestSendKeepalive_RefreshesPeerLastReadAndKeepsSessionUsable(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	before := server.LastRead()

	require.NoError(t, client.SendKeepalive())

	require.Eventually(t, func() bool {
		return server.LastRead().After(before)
	}, 2*time.Second, 5*time.Millisecond, "peer LastRead should advance on a keepalive")

	// The keepalive is liveness-only: ordinary streams still open afterwards.
	stream, err := client.OpenStream()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	accepted, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	assert.Equal(t, stream.StreamID(), accepted.StreamID())
}

func TestSendKeepalives_PingsUntilCancelled(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	before := server.LastRead()

	done := make(chan struct{})

	go func() {
		mirror.SendKeepalives(ctx, client, 5*time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return server.LastRead().After(before)
	}, 2*time.Second, 5*time.Millisecond, "keepalives should advance the peer's LastRead")

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SendKeepalives did not return after ctx cancellation")
	}
}

func TestWatchSessionLiveness_ExpiresSilentArmedPeer(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	// Arm the watchdog: one keepalive proves the client speaks the
	// protocol; then it goes silent (an unclean death).
	require.NoError(t, client.SendKeepalive())
	require.Eventually(t, func() bool {
		return server.KeepaliveSeen()
	}, 2*time.Second, 5*time.Millisecond, "the keepalive should arm the peer")

	ctx, expire := context.WithCancel(context.Background())
	defer expire()

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		// Expired: the watchdog cancelled the context because no frame
		// arrived within the timeout after the client had armed it.
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not expire a frame-silent armed session")
	}
}

func TestWatchSessionLiveness_NeverExpiresUnarmedPeer(t *testing.T) {
	t.Parallel()

	// The peer never sends a keepalive — a pre-keepalive client (an older
	// ksail over a reused agent container). Frame silence proves nothing
	// about such a client, so the watchdog must not expire it.
	_, server := newSessionPair(t)

	ctx, expire := context.WithCancel(context.Background())
	defer expire()

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		t.Fatal("watchdog expired a peer that never spoke the keepalive protocol")
	case <-time.After(300 * time.Millisecond):
		// Healthy: several timeouts elapsed without the unarmed session
		// being expired.
	}
}

func TestWatchSessionLiveness_HoldsWhileDispatchBlocked(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	// Arm the watchdog first, as a live keepalive-speaking client would.
	require.NoError(t, client.SendKeepalive())
	require.Eventually(t, func() bool {
		return server.KeepaliveSeen()
	}, 2*time.Second, 5*time.Millisecond, "the keepalive should arm the peer")

	stream, err := client.OpenStream()
	require.NoError(t, err)

	// Overfill the server-side stream's receive budget without reading it:
	// the server's demux loop parks inside dispatch on the backpressured
	// stream and can no longer consume frames — exactly the state the
	// watchdog must not confuse with a dead client.
	writeDone := make(chan struct{})

	go func() {
		defer close(writeDone)

		chunk := make([]byte, mirror.MaxTunnelPayload)
		for range 5 {
			_, writeErr := stream.Write(chunk)
			if writeErr != nil {
				return
			}
		}
	}()

	require.Eventually(t, func() bool {
		return server.DispatchInProgress()
	}, 2*time.Second, 5*time.Millisecond, "the overfilled stream should park the demux loop")

	ctx, expire := context.WithCancel(context.Background())
	defer expire()

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		t.Fatal("watchdog expired a session whose demux loop was parked on backpressure")
	case <-time.After(300 * time.Millisecond):
		// Healthy: several timeouts elapsed while the dispatch was blocked.
	}

	drainParkedStream(t, server, writeDone)
}

// drainParkedStream accepts and reads the backpressured stream so the parked
// dispatch completes and the writer goroutine finishes; the session-pair
// cleanup can then close both ends cleanly.
func drainParkedStream(t *testing.T, server *mirror.TunnelSession, writeDone <-chan struct{}) {
	t.Helper()

	acceptCtx, cancelAccept := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelAccept()

	accepted, err := server.AcceptStream(acceptCtx)
	require.NoError(t, err)

	go func() {
		buf := make([]byte, mirror.MaxTunnelPayload)
		for {
			_, readErr := accepted.Read(buf)
			if readErr != nil {
				return
			}
		}
	}()

	select {
	case <-writeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("draining the stream did not unblock the writer")
	}
}

func TestSendKeepalives_PingsImmediately(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	// An interval far beyond the assertion window proves the first ping is
	// immediate rather than ticker-driven: it is what arms the agent's
	// watchdog at session start instead of one interval in.
	go mirror.SendKeepalives(t.Context(), client, time.Hour)

	require.Eventually(t, func() bool {
		return server.KeepaliveSeen()
	}, 2*time.Second, 5*time.Millisecond, "SendKeepalives should ping before the first tick")
}

func TestWatchSessionLiveness_SurvivesWhileKeepalivesFlow(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	pingCtx, stopPings := context.WithCancel(t.Context())
	defer stopPings()

	go mirror.SendKeepalives(pingCtx, client, 10*time.Millisecond)

	ctx, expire := context.WithCancel(context.Background())
	defer expire()

	// Generous timeout relative to the ping interval so scheduler jitter
	// cannot expire a healthy session on a loaded runner.
	go mirror.WatchSessionLiveness(ctx, server, 500*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		t.Fatal("watchdog expired a session with live keepalives")
	case <-time.After(750 * time.Millisecond):
		// Healthy: pings kept the deadline refreshed past the timeout.
	}
}

func TestWatchSessionLiveness_ReturnsWhenSessionEnds(t *testing.T) {
	t.Parallel()

	_, server := newSessionPair(t)

	ctx, expire := context.WithCancel(context.Background())
	defer expire()

	done := make(chan struct{})

	go func() {
		mirror.WatchSessionLiveness(ctx, server, time.Hour, expire)
		close(done)
	}()

	require.NoError(t, server.Close())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not return after the session ended")
	}

	assert.NoError(t, ctx.Err(), "a session that ended cleanly must not be expired")
}

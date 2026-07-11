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

func TestWatchSessionLiveness_ExpiresSilentPeer(t *testing.T) {
	t.Parallel()

	_, server := newSessionPair(t)

	ctx, expire := context.WithCancel(context.Background())
	defer expire()

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		// Expired: the watchdog cancelled the context because no frame
		// arrived within the timeout.
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not expire a frame-silent session")
	}
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

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

// TestWriteReadFrame_KeepaliveRoundTrip verifies a keepalive frame survives a
// write/read round trip with stream ID 0 and an empty payload.
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

// TestWriteFrame_RejectsKeepalivePayload verifies WriteFrame refuses to emit a
// keepalive carrying a payload — keepalives are control-only frames.
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

// TestSendKeepalive_RefreshesPeerLastReadAndKeepsSessionUsable verifies a
// keepalive advances the peer's LastRead while leaving the session fully
// usable — ordinary streams still open afterwards.
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

// TestSendKeepalives_PingsUntilCancelled verifies the keepalive loop keeps
// refreshing the peer's LastRead and returns promptly once its context is
// cancelled.
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

// TestWatchSessionLiveness_ExpiresSilentArmedPeer verifies the watchdog
// cancels the context when a peer that armed it with a keepalive then goes
// frame-silent past the timeout — the unclean-death detection path.
func TestWatchSessionLiveness_ExpiresSilentArmedPeer(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	// Arm the watchdog: one keepalive proves the client speaks the
	// protocol; then it goes silent (an unclean death).
	require.NoError(t, client.SendKeepalive())
	require.Eventually(t, func() bool {
		return server.KeepaliveSeen()
	}, 2*time.Second, 5*time.Millisecond, "the keepalive should arm the peer")

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		// Expired: the watchdog cancelled the context because no frame
		// arrived within the timeout after the client had armed it.
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not expire a frame-silent armed session")
	}
}

// TestWatchSessionLiveness_ClosesSessionOnExpiry verifies an expiry tears the
// session itself down, not just the forward context — pumps blocked on the
// dead client's exec stream only unblock when the session closes.
func TestWatchSessionLiveness_ClosesSessionOnExpiry(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	require.NoError(t, client.SendKeepalive())
	require.Eventually(t, func() bool {
		return server.KeepaliveSeen()
	}, 2*time.Second, 5*time.Millisecond, "the keepalive should arm the peer")

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	// An expiry must tear the session down, not just cancel the forward
	// context: pumps blocked writing to the dead client's exec stream only
	// unblock when the session closes its channel halves, and the REDIRECT
	// teardown runs only after the forward loop returns.
	select {
	case <-server.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog expiry did not close the session")
	}
}

// TestWatchSessionLiveness_NeverExpiresUnarmedPeer verifies frame silence
// never expires a peer that has not armed the watchdog — a pre-keepalive
// client over a reused agent container must not be killed.
func TestWatchSessionLiveness_NeverExpiresUnarmedPeer(t *testing.T) {
	t.Parallel()

	// The peer never sends a keepalive — a pre-keepalive client (an older
	// ksail over a reused agent container). Frame silence proves nothing
	// about such a client, so the watchdog must not expire it.
	_, server := newSessionPair(t)

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		t.Fatal("watchdog expired a peer that never spoke the keepalive protocol")
	case <-time.After(300 * time.Millisecond):
		// Healthy: several timeouts elapsed without the unarmed session
		// being expired.
	}
}

// TestWatchSessionLiveness_HoldsWhileDispatchBlocked verifies the watchdog
// tolerates a demux loop parked on stream backpressure past the ordinary
// deadline (the bounded dispatch grace) instead of expiring a live session.
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

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

	// With a 100ms timeout the parked-dispatch grace deadline is 400ms
	// (dispatchGraceMultiplier); several plain timeouts fit inside it, so
	// holding for 250ms proves the watchdog tolerates backpressure beyond
	// the ordinary deadline without granting the indefinite immunity the
	// bounded grace replaced.
	go mirror.WatchSessionLiveness(ctx, server, 100*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		t.Fatal("watchdog expired a session whose demux loop was parked on backpressure")
	case <-time.After(250 * time.Millisecond):
		// Healthy: multiple timeouts elapsed while the dispatch was blocked.
	}

	drainParkedStream(t, server, writeDone)
}

// TestWatchSessionLiveness_ExpiresDispatchBlockedPeerAfterGrace pins the
// bound on the backpressure grace: a client that dies while a stream has the
// demux loop parked produces no further frames, so the parked dispatch never
// drains — the watchdog must still expire the session once the extended
// (timeout × dispatchGraceMultiplier) deadline passes, or the agent's
// REDIRECT rule would stay orphaned exactly as in the unarmed black-hole
// case (ksail#6040).
func TestWatchSessionLiveness_ExpiresDispatchBlockedPeerAfterGrace(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	require.NoError(t, client.SendKeepalive())
	require.Eventually(t, func() bool {
		return server.KeepaliveSeen()
	}, 2*time.Second, 5*time.Millisecond, "the keepalive should arm the peer")

	stream, err := client.OpenStream()
	require.NoError(t, err)

	// Park the server's demux loop on a backpressured stream, then go
	// silent — the unclean-death half of the scenario the bounded grace
	// exists for.
	go func() {
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

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		// Expired: the parked dispatch delayed but did not prevent the
		// liveness deadline.
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never expired a dead client parked on backpressure")
	}
}

// TestWatchSessionLiveness_ExpiresPreArmedSilentPeer covers the
// --expect-keepalives arming path: the client declared it will ping (so the
// agent pre-arms via ArmLiveness) but died before delivering its first
// keepalive — the watchdog must expire the session from LastRead's creation
// timestamp rather than waiting forever for a first frame (ksail#6040).
func TestWatchSessionLiveness_ExpiresPreArmedSilentPeer(t *testing.T) {
	t.Parallel()

	_, server := newSessionPair(t)

	server.ArmLiveness()

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

	go mirror.WatchSessionLiveness(ctx, server, 40*time.Millisecond, expire)

	select {
	case <-ctx.Done():
		// Expired: pre-arming makes total frame silence fatal after the
		// timeout, measured from the session's creation time.
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never expired a pre-armed, totally silent session")
	}
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

// TestSendKeepalives_PingsImmediately verifies the first keepalive is sent at
// once rather than one ticker interval in — it is what arms the agent's
// watchdog at session start.
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

// TestWatchSessionLiveness_SurvivesWhileKeepalivesFlow verifies flowing
// keepalives keep refreshing the deadline so a healthy session outlives the
// watchdog timeout without being expired.
func TestWatchSessionLiveness_SurvivesWhileKeepalivesFlow(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	pingCtx, stopPings := context.WithCancel(t.Context())
	defer stopPings()

	go mirror.SendKeepalives(pingCtx, client, 10*time.Millisecond)

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

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

// TestWatchSessionLiveness_ReturnsWhenSessionEnds verifies the watchdog
// returns when the session closes cleanly, without expiring its context.
func TestWatchSessionLiveness_ReturnsWhenSessionEnds(t *testing.T) {
	t.Parallel()

	_, server := newSessionPair(t)

	ctx, expire := context.WithCancelCause(context.Background())
	defer expire(nil)

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

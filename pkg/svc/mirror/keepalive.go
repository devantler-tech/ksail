package mirror

import (
	"context"
	"time"
)

// SteerKeepaliveInterval is how often the intercept client pings the steering
// agent ([TunnelSession.SendKeepalive]). It is deliberately a small fraction
// of [SteerClientLivenessTimeout] so a single delayed or dropped ping cannot
// expire a healthy session.
const SteerKeepaliveInterval = 10 * time.Second

// SteerClientLivenessTimeout is how long the steering agent tolerates a
// frame-silent channel before concluding the client is gone and tearing
// itself down (ksail#6040). The exec stream does not reliably deliver EOF
// when the client dies uncleanly (SIGKILL, network drop, laptop sleep), so
// without this deadline the agent — and its REDIRECT rule — would outlive the
// client and keep the workload's traffic black-holed. Any inbound frame
// counts as liveness, so idle sessions survive on keepalives alone while
// active ones never expire mid-transfer.
const SteerClientLivenessTimeout = 30 * time.Second

// livenessPollDivisor sets how often the watchdog samples the session's
// LastRead relative to the timeout, bounding detection latency to
// timeout + timeout/livenessPollDivisor without a busy loop.
const livenessPollDivisor = 4

// SendKeepalives pings the peer immediately and then every interval until ctx
// is cancelled, the session ends, or a ping fails to write (the session
// teardown owns surfacing that failure — a dead channel already ends the
// session). The immediate first ping arms the agent's liveness watchdog
// ([TunnelSession.KeepaliveSeen]) at session start, so the unprotected window
// is one round-trip rather than one full interval. The intercept client runs
// it as a goroutine beside the pump for the whole session lifetime — and only
// when the agent is known to speak the keepalive protocol: an older agent's
// decoder tears the tunnel down on the unknown frame type, so the client
// gates the pings on a provably-matching agent (see the intercept command's
// steerKeepaliveSupported).
func SendKeepalives(ctx context.Context, session *TunnelSession, interval time.Duration) {
	err := session.SendKeepalive()
	if err != nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-session.Done():
			return
		case <-ticker.C:
			err := session.SendKeepalive()
			if err != nil {
				return
			}
		}
	}
}

// watchSessionLiveness cancels expire once an armed session has read no frame
// for longer than timeout, and returns quietly when ctx is cancelled or the
// session ends first. RunSteerAgent runs it beside the forward loop; the
// cancelled context flows into the agent's existing reverse teardown, so a
// dead client's REDIRECT rule is removed instead of black-holing the
// workload (ksail#6040).
//
// Two guards keep it from expiring a live session:
//   - It only arms after the session has seen a keepalive
//     ([TunnelSession.KeepaliveSeen]) — a pre-keepalive client (an older
//     ksail over a reused agent container) never pings, so frame silence
//     proves nothing about it; such sessions keep the pre-keepalive
//     behaviour instead of being cut off mid-use.
//   - It holds off while a dispatch is in progress
//     ([TunnelSession.DispatchInProgress]) — a backpressured stream parks
//     the demux loop, so the client's pings sit unread in the channel;
//     silence while parked means "cannot measure", not "dead". Once the
//     dispatch drains, the queued frames are read and stamp LastRead before
//     the next tick can expire the session.
func watchSessionLiveness(
	ctx context.Context,
	session *TunnelSession,
	timeout time.Duration,
	expire context.CancelFunc,
) {
	ticker := time.NewTicker(timeout / livenessPollDivisor)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-session.Done():
			return
		case <-ticker.C:
			if !session.KeepaliveSeen() || session.DispatchInProgress() {
				continue
			}

			if time.Since(session.LastRead()) > timeout {
				expire()

				return
			}
		}
	}
}

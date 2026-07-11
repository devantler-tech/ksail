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

// SendKeepalives pings the peer every interval until ctx is cancelled, the
// session ends, or a ping fails to write (the session teardown owns
// surfacing that failure — a dead channel already ends the session). The
// intercept client runs it as a goroutine beside the pump for the whole
// session lifetime.
func SendKeepalives(ctx context.Context, session *TunnelSession, interval time.Duration) {
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

// watchSessionLiveness cancels expire once the session has read no frame for
// longer than timeout, and returns quietly when ctx is cancelled or the
// session ends first. RunSteerAgent runs it beside the forward loop; the
// cancelled context flows into the agent's existing reverse teardown, so a
// dead client's REDIRECT rule is removed instead of black-holing the
// workload (ksail#6040).
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
			if time.Since(session.LastRead()) > timeout {
				expire()

				return
			}
		}
	}
}

package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

// pumpDirections sizes the result channel so both copy goroutines can finish
// without a receiver.
const pumpDirections = 2

// LocalDialer opens one connection to the developer's local process for one
// intercepted stream. It is a seam so the serve loop is testable against
// in-memory pipes and the CLI wiring can plug in a real TCP dial.
type LocalDialer func(ctx context.Context) (io.ReadWriteCloser, error)

// Pump bridges one intercepted connection with its tunnel stream: bytes are
// copied in both directions until either side ends, then BOTH halves are
// closed — a dropped local process must never leave the redirected cluster
// connection half-open (#5839's reversibility posture applied to data flow).
// It returns the error that ended the first direction (nil for a clean EOF);
// whatever the closes provoke in the other direction is teardown noise and is
// discarded.
func Pump(left, right io.ReadWriteCloser) error {
	results := make(chan error, pumpDirections)

	go copyDirection(results, left, right)
	go copyDirection(results, right, left)

	firstErr := <-results

	_ = left.Close()
	_ = right.Close()

	// Wait for the second direction to unwind against the closed halves so
	// Pump never leaks a copy goroutine.
	<-results

	return firstErr
}

// copyDirection copies one direction of a pump and reports how it ended.
func copyDirection(results chan<- error, dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	results <- err
}

// ForwardRedirected is the steering agent's side of intercept: it accepts each
// connection the iptables REDIRECT delivers to the listener, opens one tunnel
// stream for it, and pumps the two together. It blocks until the context is
// cancelled (returns nil), the session ends (returns nil — the tunnel ending
// is the session owner's event to inspect via [TunnelSession.Err]), or the
// listener fails (returns the error). Both stop signals close the listener so
// a parked Accept notices them without another connection having to arrive.
// Cancellation also tears down every active pump, and all pumps are drained
// before it returns.
func ForwardRedirected(ctx context.Context, listener net.Listener, session *TunnelSession) error {
	stopWatch := unblockAcceptOnStop(ctx, session, listener)
	defer stopWatch()

	pumps := &sync.WaitGroup{}
	defer pumps.Wait()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if stopRequested(ctx, session, err) {
				return nil
			}

			return fmt.Errorf("accepting a redirected connection: %w", err)
		}

		stream, err := session.OpenStream()
		if err != nil {
			_ = conn.Close()

			if stopRequested(ctx, session, err) {
				return nil
			}

			return fmt.Errorf("opening a tunnel stream for a redirected connection: %w", err)
		}

		pumps.Go(func() {
			pumpUntilDone(ctx, conn, stream)
		})
	}
}

// unblockAcceptOnStop closes the listener when either stop signal fires — the
// context ending or the tunnel session ending — so a parked Accept notices a
// stop without another connection having to arrive. The returned stop
// function releases the watcher once the accept loop has returned.
func unblockAcceptOnStop(
	ctx context.Context,
	session *TunnelSession,
	listener net.Listener,
) func() {
	watchDone := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-session.Done():
			_ = listener.Close()
		case <-watchDone:
		}
	}()

	return func() { close(watchDone) }
}

// stopRequested reports whether an accept-loop failure was caused by a stop
// signal — the context ending or the session ending — rather than being a
// genuine failure.
func stopRequested(ctx context.Context, session *TunnelSession, err error) bool {
	return ctx.Err() != nil || sessionEnded(session) || errors.Is(err, ErrTunnelSessionClosed)
}

// sessionEnded reports whether the session's demux loop has exited — the
// signal the listener watcher acts on, so an Accept it unblocked can tell a
// session end from a genuine listener failure.
func sessionEnded(session *TunnelSession) bool {
	select {
	case <-session.Done():
		return true
	default:
		return false
	}
}

// pumpUntilDone runs one pump and tears both halves down early when the
// context ends, so a cancelled intercept session never leaves a pump — and
// the loop draining it — blocked on a still-open connection.
func pumpUntilDone(ctx context.Context, left, right io.ReadWriteCloser) {
	stopTeardown := context.AfterFunc(ctx, func() {
		_ = left.Close()
		_ = right.Close()
	})
	defer stopTeardown()

	_ = Pump(left, right)
}

// ServeIntercepted is the local ksail side of intercept: it accepts each
// stream the steering agent opened, dials the developer's local process, and
// pumps the two together. A failed dial closes the stream — the tunnel's
// "connection refused", which makes the agent close the redirected cluster
// connection. It blocks until the context is cancelled or the session ends: a
// clean end (context cancelled or the caller closed the session) returns nil,
// while a session the demux loop tore down on a protocol/codec error returns
// that error. Cancellation also tears down every active pump, and all pumps are
// drained before it returns.
func ServeIntercepted(ctx context.Context, session *TunnelSession, dial LocalDialer) error {
	pumps := &sync.WaitGroup{}
	defer pumps.Wait()

	for {
		stream, err := session.AcceptStream(ctx)
		if err != nil {
			// AcceptStream fails both on a deliberate end of serving (the context
			// ended or the caller closed the session — nil) and when the demux
			// loop tore the session down on a protocol/codec error (e.g. non-frame
			// bytes on the steering channel from a noisy agent image). Surface the
			// latter so a corrupted tunnel is a diagnosable failure, never a silent
			// success.
			return sessionTeardownErr(session)
		}

		pumps.Go(func() {
			serveOneStream(ctx, stream, dial)
		})
	}
}

// sessionTeardownErr reports the demux loop's terminal error when the session
// has already torn itself down — its Done channel is closed and Err is non-nil.
// teardown records Err before closing Done, so a closed Done means Err is final.
// A caller-initiated Close or a context cancellation leaves Err nil (Done may
// still be open), both of which are a clean stop and yield nil here.
func sessionTeardownErr(session *TunnelSession) error {
	select {
	case <-session.Done():
		loopErr := session.Err()
		if loopErr != nil {
			return fmt.Errorf("steering tunnel torn down: %w", loopErr)
		}
	default:
	}

	return nil
}

// serveOneStream dials the local process for one intercepted stream and pumps
// until either side ends.
func serveOneStream(ctx context.Context, stream *TunnelStream, dial LocalDialer) {
	conn, err := dial(ctx)
	if err != nil {
		_ = stream.Close()

		return
	}

	pumpUntilDone(ctx, stream, conn)
}

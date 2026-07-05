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
// cancelled (returns nil — the deliberate stop closes the listener), the
// session closes (returns nil — the tunnel ending is the session owner's event
// to inspect via [TunnelSession.Err]), or the listener fails (returns the
// error). Cancellation also tears down every active pump, and all pumps are
// drained before it returns.
func ForwardRedirected(ctx context.Context, listener net.Listener, session *TunnelSession) error {
	stopClosing := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stopClosing()

	pumps := &sync.WaitGroup{}
	defer pumps.Wait()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("accepting a redirected connection: %w", err)
		}

		stream, err := session.OpenStream()
		if err != nil {
			_ = conn.Close()

			if ctx.Err() != nil || errors.Is(err, ErrTunnelSessionClosed) {
				return nil
			}

			return fmt.Errorf("opening a tunnel stream for a redirected connection: %w", err)
		}

		pumps.Go(func() {
			pumpUntilDone(ctx, conn, stream)
		})
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
// connection. It blocks until the context is cancelled or the session closes
// (both return nil — inspect [TunnelSession.Err] for why a session ended);
// cancellation also tears down every active pump, and all pumps are drained
// before it returns.
func ServeIntercepted(ctx context.Context, session *TunnelSession, dial LocalDialer) error {
	pumps := &sync.WaitGroup{}
	defer pumps.Wait()

	for {
		stream, err := session.AcceptStream(ctx)
		if err != nil {
			// AcceptStream only fails when the session closed or the context
			// ended — both are a deliberate end of serving, not a failure.
			return nil
		}

		pumps.Go(func() {
			serveOneStream(ctx, stream, dial)
		})
	}
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

package sshbootstrap

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Cancel precisely after SSH has opened the channel, keeping the result arm
// selected while exercising real context cancellation at that race boundary.
type cancelOnSessionOpenContext struct {
	context.Context //nolint:containedctx // Test implementation of context.Context.

	cancel context.CancelFunc
	opened <-chan struct{}
}

func (c cancelOnSessionOpenContext) Err() error {
	select {
	case <-c.opened:
		c.cancel()
	default:
	}

	return c.Context.Err() //nolint:wrapcheck // Context.Err must preserve its sentinel.
}

type closeGateChannel struct {
	ssh.Channel

	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *closeGateChannel) Close() error {
	c.once.Do(func() { close(c.started) })
	<-c.release

	return nil
}

type sessionRaceConn struct {
	ssh.Conn

	channel  *closeGateChannel
	opened   chan struct{}
	requests chan *ssh.Request
	done     chan struct{}
	once     sync.Once
}

func (c *sessionRaceConn) OpenChannel(string, []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	close(c.opened)

	return c.channel, c.requests, nil
}

func (c *sessionRaceConn) Close() error {
	c.once.Do(func() { close(c.done) })

	return nil
}

func (c *sessionRaceConn) Wait() error {
	<-c.done

	return nil
}

func TestOpenSessionCancellationDoesNotWaitForChannelClose(t *testing.T) {
	t.Parallel()

	channel := &closeGateChannel{started: make(chan struct{}), release: make(chan struct{})}
	conn := &sessionRaceConn{
		channel: channel, opened: make(chan struct{}),
		requests: make(chan *ssh.Request), done: make(chan struct{}),
	}
	channels := make(chan ssh.NewChannel)
	requests := make(chan *ssh.Request)

	close(channels)
	close(requests)

	client := &Client{conn: ssh.NewClient(conn, channels, requests)}

	t.Cleanup(func() {
		close(channel.release)
		close(conn.requests)

		_ = client.Close()
	})

	parent, cancel := context.WithCancel(t.Context())
	defer cancel()

	ctx := cancelOnSessionOpenContext{Context: parent, cancel: cancel, opened: conn.opened}
	done := make(chan error, 1)

	go func() {
		_, err := client.openSession(ctx)
		done <- err
	}()

	select {
	case <-channel.started:
	case <-time.After(5 * time.Second):
		t.Fatal("opened session was not closed after cancellation")
	}

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("openSession error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("openSession waited for a blocked channel close after cancellation")
	}
}

type countingCloser struct {
	closes *atomic.Int32
}

func (c countingCloser) Close() error {
	c.closes.Add(1)

	return nil
}

// TestCloseOnDoneIgnoresContextEndingAfterStop stops the watch and then ends the
// context before its goroutine has run, as a dial does when it returns a client
// and its caller cancels the dial's context. The goroutine then finds both
// signals ready, and the client it was watching must stay open.
func TestCloseOnDoneIgnoresContextEndingAfterStop(t *testing.T) {
	t.Parallel()

	const attempts = 2000

	var closes atomic.Int32

	for range attempts {
		ctx, cancel := context.WithCancel(t.Context())
		stop := closeOnDone(ctx, countingCloser{closes: &closes})

		stop()
		cancel()
	}

	time.Sleep(100 * time.Millisecond)

	if got := closes.Load(); got != 0 {
		t.Fatalf("closed %d of %d stopped watches", got, attempts)
	}
}

// TestCloseOnDoneClosesWhenContextEndsBeforeStop keeps cancellation working: a
// hung handshake or command is only unblocked by this close.
func TestCloseOnDoneClosesWhenContextEndsBeforeStop(t *testing.T) {
	t.Parallel()

	var closes atomic.Int32

	ctx, cancel := context.WithCancel(t.Context())
	stop := closeOnDone(ctx, countingCloser{closes: &closes})

	defer stop()

	cancel()

	deadline := time.Now().Add(5 * time.Second)
	for closes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("a watch whose context ended did not close")
		}

		time.Sleep(time.Millisecond)
	}
}

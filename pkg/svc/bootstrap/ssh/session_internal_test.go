package sshbootstrap

import (
	"context"
	"errors"
	"sync"
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

package sshbootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	sshbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/ssh"
	"golang.org/x/crypto/ssh"
)

// startSilentSessionServer runs an SSH server that completes the handshake but
// never answers a session channel-open request, so the client's session open
// blocks until something interrupts it.
func startSilentSessionServer(t *testing.T, clientKey ssh.PublicKey) (string, ssh.PublicKey) {
	t.Helper()

	hostPair, err := sshbootstrap.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if !bytes.Equal(key.Marshal(), clientKey.Marshal()) {
				return nil, errUnknownClientKey
			}

			return &ssh.Permissions{}, nil
		},
	}
	config.AddHostKey(hostPair.Signer)

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			netConn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func() {
				_, channels, requests, connErr := ssh.NewServerConn(netConn, config)
				if connErr != nil {
					return
				}

				go ssh.DiscardRequests(requests)

				// Take each channel-open request and never answer it.
				for newChannel := range channels {
					_ = newChannel.ChannelType()
				}
			}()
		}
	}()

	return listener.Addr().String(), hostPair.Signer.PublicKey()
}

// TestRunHonoursContextWhileOpeningSession pins that a session open the server
// never answers still ends when the caller's context does, so a bring-up stage
// deadline bounds every probe rather than only the command it runs.
func TestRunHonoursContextWhileOpeningSession(t *testing.T) {
	t.Parallel()

	pair := mustGenerateKeyPair(t)
	addr, hostKey := startSilentSessionServer(t, pair.Signer.PublicKey())
	client := mustDial(t, addr, pair, hostKey)

	ctx, cancel := context.WithTimeout(t.Context(), testCancelBudget)
	defer cancel()

	done := make(chan error, 1)

	go func() {
		_, runErr := client.Run(ctx, "true")
		done <- runErr
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Run error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(testRunBudget):
		t.Fatal("Run did not return after its context ended while opening a session")
	}
}

// Command ksail-desktop runs the KSail web UI in a native desktop window.
//
// It reuses the same in-process server as `ksail ui` (pkg/cli/uiserver): the server serves the
// embedded SPA and a REST API backed by the local cluster lifecycle on a loopback port, and a native
// system webview (no bundled browser engine) renders it. Closing the window stops the server.
//
// This is a separate Go module so its CGO/webview dependency stays out of the main, statically
// linked `ksail` binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/uiserver"
	//nolint:depguard // webview is the desktop app's UI runtime; the CLI module's allowlist does not apply here.
	webview "github.com/webview/webview_go"
)

const (
	windowTitle    = "KSail"
	windowWidth    = 1100
	windowHeight   = 820
	readyTimeout   = 30 * time.Second
	readyPollEvery = 200 * time.Millisecond
)

var errServerNotReady = errors.New("ksail ui server did not become ready")

func main() {
	err := run()
	if err != nil {
		log.Fatalf("ksail-desktop: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, url, err := uiserver.Listen(ctx, 0)
	if err != nil {
		return fmt.Errorf("start ui server: %w", err)
	}

	server := uiserver.NewServer()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx, listener) }()

	readyErr := waitForReady(ctx, url+"readyz")
	if readyErr != nil {
		return readyErr
	}

	view := webview.New(false)
	defer view.Destroy()

	view.SetTitle(windowTitle)
	view.SetSize(windowWidth, windowHeight, webview.HintNone)
	view.Navigate(url)
	view.Run() // blocks until the window is closed

	// Stop the server and surface any non-graceful shutdown error.
	cancel()

	shutdownErr := <-serveErr
	if shutdownErr != nil {
		return fmt.Errorf("ui server: %w", shutdownErr)
	}

	return nil
}

// waitForReady polls the server's /readyz endpoint until it returns 200 or the timeout elapses.
func waitForReady(ctx context.Context, readyURL string) error {
	deadline := time.Now().Add(readyTimeout)

	for {
		if probeReady(ctx, readyURL) {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%w within %s", errServerNotReady, readyTimeout)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for ui server: %w", ctx.Err())
		case <-time.After(readyPollEvery):
		}
	}
}

func probeReady(ctx context.Context, readyURL string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
	if err != nil {
		return false
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}

	defer func() { _ = response.Body.Close() }()

	return response.StatusCode == http.StatusOK
}

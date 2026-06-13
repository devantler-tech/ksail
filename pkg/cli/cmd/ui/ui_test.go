package ui_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const uiShutdownTimeout = 5 * time.Second

// syncBuffer is a goroutine-safe buffer so a test can read command output while the command writes
// it from another goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	//nolint:wrapcheck // bytes.Buffer.Write never returns an error.
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestNewUICmdFlagsAndAnnotations(t *testing.T) {
	t.Parallel()

	cmd := ui.NewUICmd()

	assert.Equal(t, "ui", cmd.Name())
	assert.Equal(t, "true", cmd.Annotations[annotations.AnnotationExclude])

	portFlag := cmd.Flags().Lookup("port")
	require.NotNil(t, portFlag)
	assert.Equal(t, "0", portFlag.DefValue)

	noBrowserFlag := cmd.Flags().Lookup("no-browser")
	require.NotNil(t, noBrowserFlag)
	assert.Equal(t, "false", noBrowserFlag.DefValue)
}

//nolint:paralleltest // mutates the package-level browser launcher; must run serially.
func TestUICmdServesOpensBrowserAndShutsDown(t *testing.T) {
	openedURL := make(chan string, 1)
	restore := ui.SetOpenBrowser(func(_ context.Context, url string) error {
		openedURL <- url

		return nil
	})

	defer restore()

	cmd := ui.NewUICmd()

	output := &syncBuffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"--port", "0"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd.SetContext(ctx)

	done := make(chan error, 1)

	go func() { done <- cmd.Execute() }()

	select {
	case url := <-openedURL:
		assert.True(
			t,
			strings.HasPrefix(url, "http://127.0.0.1:"),
			"browser should open a loopback URL, got %q",
			url,
		)
	case <-time.After(uiShutdownTimeout):
		cancel()
		t.Fatal("browser was not opened")
	}

	cancel()

	requireCommandExit(t, done)

	assert.Contains(t, output.String(), "KSAIL_UI_URL=http://127.0.0.1:")
}

//nolint:paralleltest // mutates the package-level browser launcher; must run serially.
func TestUICmdNoBrowserSkipsOpen(t *testing.T) {
	var opened atomic.Bool

	restore := ui.SetOpenBrowser(func(_ context.Context, _ string) error {
		opened.Store(true)

		return nil
	})

	defer restore()

	cmd := ui.NewUICmd()

	output := &syncBuffer{}
	cmd.SetOut(output)
	cmd.SetErr(output)
	cmd.SetArgs([]string{"--no-browser", "--port", "0"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd.SetContext(ctx)

	done := make(chan error, 1)

	go func() { done <- cmd.Execute() }()

	require.Eventually(t, func() bool {
		return strings.Contains(output.String(), "KSAIL_UI_URL=")
	}, uiShutdownTimeout, 10*time.Millisecond)

	cancel()

	requireCommandExit(t, done)

	assert.False(t, opened.Load(), "browser must not be opened with --no-browser")
	assert.Contains(t, output.String(), "KSAIL_UI_URL=http://127.0.0.1:")
}

func requireCommandExit(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(uiShutdownTimeout):
		t.Fatal("command did not shut down after context cancellation")
	}
}

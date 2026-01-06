package notify_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/gkampitakis/go-snaps/snaps"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

// Static errors for testing.
var (
	errTestInstallationFailed = errors.New("installation failed")
	errTestTaskFailed         = errors.New("failed")
)

func TestProgressGroup_EmptyTasks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup("Installing", "📦", &buf)

	err := progressGroup.Run(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty tasks, got: %q", buf.String())
	}
}

func TestProgressGroup_SingleTaskSuccess(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup(
		"Installing",
		"📦",
		&buf,
		notify.WithLabels(notify.InstallingLabels()),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "test-component",
			Fn: func(_ context.Context) error {
				return nil
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	snaps.MatchSnapshot(t, buf.String())
}

func TestProgressGroup_SingleTaskFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup("Installing", "📦", &buf)

	tasks := []notify.ProgressTask{
		{
			Name: "failing-component",
			Fn: func(_ context.Context) error {
				return errTestInstallationFailed
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err == nil {
		t.Error("expected error, got nil")
	}

	snaps.MatchSnapshot(t, buf.String())
}

func TestProgressGroup_MultipleTasksSuccess(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup(
		"Installing",
		"📦",
		&buf,
		notify.WithLabels(notify.InstallingLabels()),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "component-a",
			Fn: func(_ context.Context) error {
				time.Sleep(10 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "component-b",
			Fn: func(_ context.Context) error {
				time.Sleep(10 * time.Millisecond)

				return nil
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	snaps.MatchSnapshot(t, buf.String())
}

func TestProgressGroup_PartialFailure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup("Installing", "📦", &buf)

	tasks := []notify.ProgressTask{
		{
			Name: "good-component",
			Fn: func(_ context.Context) error {
				return nil
			},
		},
		{
			Name: "bad-component",
			Fn: func(_ context.Context) error {
				return errTestTaskFailed
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err == nil {
		t.Error("expected error, got nil")
	}

	snaps.MatchSnapshot(t, buf.String())
}

func TestProgressGroup_ContextCancellation(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup("Installing", "📦", &buf)

	ctx, cancel := context.WithCancel(context.Background())

	tasks := []notify.ProgressTask{
		{
			Name: "long-running",
			Fn: func(taskCtx context.Context) error {
				select {
				case <-taskCtx.Done():
					return taskCtx.Err()
				case <-time.After(10 * time.Second):
					return nil
				}
			},
		},
	}

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := progressGroup.Run(ctx, tasks...)
	if err == nil {
		t.Error("expected error due to cancellation, got nil")
	}

	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

func TestProgressGroup_DefaultWriter(t *testing.T) {
	t.Parallel()

	// Test that nil writer defaults to os.Stdout (just ensure no panic)
	progressGroup := notify.NewProgressGroup("Installing", "", nil)

	// Run with empty tasks to verify no panic
	err := progressGroup.Run(context.Background())
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestProgressGroup_DefaultLabels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// Use default labels (pending, running, completed)
	progressGroup := notify.NewProgressGroup("Processing", "►", &buf)

	tasks := []notify.ProgressTask{
		{
			Name: "task-1",
			Fn: func(_ context.Context) error {
				return nil
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	snaps.MatchSnapshot(t, buf.String())
}

func TestProgressGroup_ValidatingLabels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup(
		"Validating",
		"✅",
		&buf,
		notify.WithLabels(notify.ValidatingLabels()),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "schema",
			Fn: func(_ context.Context) error {
				return nil
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	snaps.MatchSnapshot(t, buf.String())
}

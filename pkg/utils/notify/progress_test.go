package notify_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
)

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

	output := buf.String()
	if !strings.Contains(output, "test-component") {
		t.Errorf("expected output to contain 'test-component', got: %q", output)
	}

	if !strings.Contains(output, "installed") {
		t.Errorf("expected output to contain 'installed', got: %q", output)
	}
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

	if !strings.Contains(err.Error(), "failing-component") {
		t.Errorf("expected error to contain task name, got: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "failed") {
		t.Errorf("expected output to contain 'failed', got: %q", output)
	}
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

	output := buf.String()
	if !strings.Contains(output, "component-a") {
		t.Errorf("expected output to contain 'component-a', got: %q", output)
	}

	if !strings.Contains(output, "component-b") {
		t.Errorf("expected output to contain 'component-b', got: %q", output)
	}

	if !strings.Contains(output, "installed") {
		t.Errorf("expected output to contain 'installed', got: %q", output)
	}
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

	if !strings.Contains(err.Error(), "bad-component") {
		t.Errorf("expected error to contain 'bad-component', got: %v", err)
	}
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

	output := buf.String()
	if !strings.Contains(output, "completed") {
		t.Errorf("expected output to contain 'completed' (default label), got: %q", output)
	}
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

	output := buf.String()
	if !strings.Contains(output, "validated") {
		t.Errorf("expected output to contain 'validated', got: %q", output)
	}
}

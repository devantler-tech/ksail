package notify_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/gkampitakis/go-snaps/snaps"
)

var (
	errTaskA = errors.New("task-a error")
	errTaskC = errors.New("task-c error")
)

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

// Static errors for testing.
var (
	errTestInstallationFailed = errors.New("installation failed")
	errTestTaskFailed         = errors.New("failed")
)

// fakeClock is a deterministic clock for testing per-task duration tracking.
type fakeClock struct {
	mu      sync.Mutex
	current time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{current: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.current
}

func (c *fakeClock) Since(t time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.current.Sub(t)
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.current = c.current.Add(d)
}

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

	// Since tasks run concurrently, verify content without depending on order.
	output := buf.String()

	// Verify title is first
	if !strings.HasPrefix(output, "📦 Installing...\n") {
		t.Errorf("expected output to start with title, got: %q", output)
	}

	// Verify both components are started and completed
	expectedPatterns := []string{
		"► component-a installing",
		"✔ component-a installed",
		"► component-b installing",
		"✔ component-b installed",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(output, pattern) {
			t.Errorf("expected output to contain %q, got: %q", pattern, output)
		}
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
				time.Sleep(10 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "bad-component",
			Fn: func(_ context.Context) error {
				time.Sleep(10 * time.Millisecond)

				return errTestTaskFailed
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err == nil {
		t.Error("expected error, got nil")
	}

	// Since tasks run concurrently, verify content without depending on order.
	output := buf.String()

	// Verify title is first
	if !strings.HasPrefix(output, "📦 Installing...\n") {
		t.Errorf("expected output to start with title, got: %q", output)
	}

	// Verify good component started and completed
	if !strings.Contains(output, "► good-component running") {
		t.Errorf("expected good-component running message, got: %q", output)
	}

	if !strings.Contains(output, "✔ good-component completed") {
		t.Errorf("expected good-component completed message, got: %q", output)
	}

	// Verify bad component started and failed
	if !strings.Contains(output, "► bad-component running") {
		t.Errorf("expected bad-component running message, got: %q", output)
	}

	if !strings.Contains(output, "✗ bad-component failed") {
		t.Errorf("expected bad-component failed message, got: %q", output)
	}
}

func TestProgressGroup_ContextCancellation(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup("Installing", "📦", &buf)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

func TestProgressGroup_WithTimer_Success(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tmr := &fixedTimer{total: 3 * time.Second, stage: 500 * time.Millisecond}

	progressGroup := notify.NewProgressGroup(
		"Installing",
		"📦",
		&buf,
		notify.WithLabels(notify.InstallingLabels()),
		notify.WithTimer(tmr),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "component",
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

	// Should contain timing info on success
	if !strings.Contains(output, "⏲ current:") {
		t.Errorf("expected timing info in output, got: %q", output)
	}

	if !strings.Contains(output, "total:") {
		t.Errorf("expected total timing in output, got: %q", output)
	}
}

func TestProgressGroup_WithTimer_PerTaskDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"fast-component", 2 * time.Second, "fast-component installed [2s]"},
		{"slow-component", 5 * time.Second, "slow-component installed [5s]"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			tmr := &fixedTimer{total: 3 * time.Second, stage: 500 * time.Millisecond}
			clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

			progressGroup := notify.NewProgressGroup(
				"Installing",
				"📦",
				&buf,
				notify.WithLabels(notify.InstallingLabels()),
				notify.WithTimer(tmr),
				notify.WithClock(clk),
			)

			tasks := []notify.ProgressTask{
				{
					Name: test.name,
					Fn: func(_ context.Context) error {
						clk.Advance(test.duration)

						return nil
					},
				},
			}

			err := progressGroup.Run(context.Background(), tasks...)
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}

			output := buf.String()

			if !strings.Contains(output, test.expected) {
				t.Errorf("expected per-task duration %q, got: %q", test.expected, output)
			}

			if !strings.Contains(output, "⏲ current:") {
				t.Errorf("expected group timing info in output, got: %q", output)
			}
		})
	}
}

func TestProgressGroup_WithoutTimer_NoDuration(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	progressGroup := notify.NewProgressGroup(
		"Installing",
		"📦",
		&buf,
		notify.WithLabels(notify.InstallingLabels()),
		notify.WithClock(clk),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "component",
			Fn: func(_ context.Context) error {
				clk.Advance(time.Second)

				return nil
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()

	// Without timer, completion line should NOT contain duration brackets
	if strings.Contains(output, "installed [") {
		t.Errorf("expected no per-task duration without timer, got: %q", output)
	}
}

func TestProgressGroup_WithTimer_Failure_NoTiming(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tmr := &fixedTimer{total: 3 * time.Second, stage: 500 * time.Millisecond}

	progressGroup := notify.NewProgressGroup(
		"Installing",
		"📦",
		&buf,
		notify.WithTimer(tmr),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "failing-task",
			Fn: func(_ context.Context) error {
				return errTestTaskFailed
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err == nil {
		t.Error("expected error, got nil")
	}

	output := buf.String()

	// Should NOT contain timing info on failure
	if strings.Contains(output, "⏲ current:") {
		t.Errorf("expected no timing info on failure, got: %q", output)
	}
}

func TestProgressGroup_DefaultEmoji(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// Empty emoji should default to ►
	progressGroup := notify.NewProgressGroup("Processing", "", &buf)

	tasks := []notify.ProgressTask{
		{
			Name: "task",
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

	if !strings.HasPrefix(output, "► Processing...\n") {
		t.Errorf("expected default emoji ►, got: %q", output)
	}
}

func TestProgressGroup_MultipleTasksPartialOrder(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup(
		"Deploying",
		"🚀",
		&buf,
		notify.WithLabels(notify.DefaultLabels()),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "first",
			Fn: func(_ context.Context) error {
				time.Sleep(20 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "second",
			Fn: func(_ context.Context) error {
				time.Sleep(10 * time.Millisecond)

				return nil
			},
		},
		{
			Name: "third",
			Fn: func(_ context.Context) error {
				time.Sleep(5 * time.Millisecond)

				return nil
			},
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()

	// All tasks should be completed
	for _, name := range []string{"first", "second", "third"} {
		if !strings.Contains(output, "✔ "+name+" completed") {
			t.Errorf("expected completion of %q in output, got: %q", name, output)
		}
	}
}

// TestProgressGroup_InstallingLabels verifies that InstallingLabels returns
// labels with "installing" as the running state and "installed" as completed.
func TestProgressGroup_InstallingLabels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	labels := notify.InstallingLabels()
	progressGroup := notify.NewProgressGroup("Installing", "📦", &buf, notify.WithLabels(labels))

	task := notify.ProgressTask{
		Name: "my-component",
		Fn:   func(_ context.Context) error { return nil },
	}

	err := progressGroup.Run(context.Background(), task)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "installing") {
		t.Errorf("expected 'installing' label in output, got: %q", output)
	}

	if !strings.Contains(output, "installed") {
		t.Errorf("expected 'installed' label in output, got: %q", output)
	}
}

// TestProgressGroup_ReconcilingLabels verifies that ReconcilingLabels returns
// labels with "reconciling" as the running state and "reconciled" as completed.
func TestProgressGroup_ReconcilingLabels(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	labels := notify.ReconcilingLabels()
	progressGroup := notify.NewProgressGroup("Reconciling", "🔄", &buf, notify.WithLabels(labels))

	task := notify.ProgressTask{
		Name: "my-kustomization",
		Fn:   func(_ context.Context) error { return nil },
	}

	err := progressGroup.Run(context.Background(), task)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "reconciling") {
		t.Errorf("expected 'reconciling' label in output, got: %q", output)
	}

	if !strings.Contains(output, "reconciled") {
		t.Errorf("expected 'reconciled' label in output, got: %q", output)
	}
}

// TestProgressGroup_EmptyTitle verifies that ProgressGroup with an empty title
// does not print a title line.
func TestProgressGroup_EmptyTitle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup("", "", &buf)

	task := notify.ProgressTask{
		Name: "my-task",
		Fn:   func(_ context.Context) error { return nil },
	}

	err := progressGroup.Run(context.Background(), task)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()

	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("expected task output, got: %q", output)
	}

	// With an empty title, the first rendered line should be the task output,
	// not a title line such as " ..." or "► ...".
	if strings.HasPrefix(lines[0], " ...") || strings.TrimSpace(lines[0]) == "..." {
		t.Errorf(
			"expected no title line when title is empty, got first line %q in output %q",
			lines[0],
			output,
		)
	}

	if strings.HasPrefix(lines[0], "► ...") {
		t.Errorf(
			"expected no title line when title is empty, got first line %q in output %q",
			lines[0],
			output,
		)
	}

	if !strings.Contains(lines[0], "my-task") {
		t.Errorf(
			"expected first line to contain task name %q, got first line %q in output %q",
			"my-task",
			lines[0],
			output,
		)
	}
}

// TestProgressGroup_ContinueOnError verifies that WithContinueOnError makes the
// group run all tasks even when some fail, collecting all errors.
func TestProgressGroup_ContinueOnError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup(
		"Installing", "📦", &buf,
		notify.WithContinueOnError(),
	)

	tasks := []notify.ProgressTask{
		{
			Name: "task-a",
			Fn:   func(_ context.Context) error { return errTaskA },
		},
		{
			Name: "task-b",
			Fn:   func(_ context.Context) error { return nil },
		},
		{
			Name: "task-c",
			Fn:   func(_ context.Context) error { return errTaskC },
		},
	}

	err := progressGroup.Run(context.Background(), tasks...)

	// All tasks run; combined error should contain both failures.
	if err == nil {
		t.Fatal("expected combined error from two failing tasks, got nil")
	}

	if !strings.Contains(err.Error(), "task-a") {
		t.Errorf("expected task-a error in combined error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "task-c") {
		t.Errorf("expected task-c error in combined error, got: %v", err)
	}

	output := buf.String()

	// task-b should still have completed successfully despite the other failures.
	if !strings.Contains(output, "task-b") {
		t.Errorf("expected task-b output even after other tasks failed, got: %q", output)
	}
}

// TestProgressGroup_WithConcurrency verifies that WithConcurrency limits the
// number of concurrently running tasks.
func TestProgressGroup_WithConcurrency(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	const limit = 2

	progressGroup := notify.NewProgressGroup(
		"Installing", "📦", &buf,
		notify.WithConcurrency(limit),
	)

	var (
		mutex   sync.Mutex
		maxSeen int
		active  int
	)

	makeTask := func(name string) notify.ProgressTask {
		return notify.ProgressTask{
			Name: name,
			Fn: func(_ context.Context) error {
				mutex.Lock()

				active++
				if active > maxSeen {
					maxSeen = active
				}
				mutex.Unlock()

				time.Sleep(20 * time.Millisecond)

				mutex.Lock()
				active--
				mutex.Unlock()

				return nil
			},
		}
	}

	tasks := []notify.ProgressTask{
		makeTask("t1"),
		makeTask("t2"),
		makeTask("t3"),
		makeTask("t4"),
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	if maxSeen > limit {
		t.Errorf("expected at most %d concurrent tasks, saw %d", limit, maxSeen)
	}
}

// TestProgressGroup_WithCountLabel verifies that WithCountLabel changes the
// progress counter prefix in the output.
func TestProgressGroup_WithCountLabel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup(
		"Installing", "📦", &buf,
		notify.WithCountLabel("step"),
	)

	tasks := []notify.ProgressTask{
		{Name: "first", Fn: func(_ context.Context) error { return nil }},
		{Name: "second", Fn: func(_ context.Context) error { return nil }},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "step") {
		t.Errorf("expected custom count label 'step' in output, got: %q", output)
	}
}

// TestProgressGroup_WithAppendOnly verifies that WithAppendOnly suppresses the
// "► taskname running" prefix lines that CI mode normally prints.
func TestProgressGroup_WithAppendOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	progressGroup := notify.NewProgressGroup(
		"Installing", "📦", &buf,
		notify.WithAppendOnly(),
	)

	tasks := []notify.ProgressTask{
		{Name: "silent-task", Fn: func(_ context.Context) error { return nil }},
	}

	err := progressGroup.Run(context.Background(), tasks...)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	output := buf.String()

	// In append-only mode the "► taskname running" line is suppressed.
	if strings.Contains(output, "► silent-task running") {
		t.Errorf("append-only mode should suppress running prefix, got: %q", output)
	}
}

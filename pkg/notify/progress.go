package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/timer"
	fcolor "github.com/fatih/color"
	"golang.org/x/sync/errgroup"
	"golang.org/x/term"
)

// ProgressLabels defines the text labels for each task state.
// Use this to customize the status text for different contexts.
type ProgressLabels struct {
	// Pending is shown for tasks that haven't started yet (default: "pending").
	Pending string
	// Running is shown for tasks currently executing (default: "running").
	Running string
	// Completed is shown for tasks that finished successfully (default: "completed").
	Completed string
}

// DefaultLabels returns the default progress labels.
func DefaultLabels() ProgressLabels {
	return ProgressLabels{
		Pending:   "pending",
		Running:   "running",
		Completed: "completed",
	}
}

// InstallingLabels returns labels suitable for installation tasks.
func InstallingLabels() ProgressLabels {
	return ProgressLabels{
		Pending:   "pending",
		Running:   "installing",
		Completed: "installed",
	}
}

// ValidatingLabels returns labels suitable for validation tasks.
func ValidatingLabels() ProgressLabels {
	return ProgressLabels{
		Pending:   "pending",
		Running:   "validating",
		Completed: "validated",
	}
}

// ProgressTask represents a named task to be executed with progress tracking.
type ProgressTask struct {
	// Name is the display name of the task (e.g., "metrics-server", "argocd").
	Name string
	// Fn is the function to execute. It receives a context for cancellation.
	Fn func(ctx context.Context) error
}

// ProgressGroup manages parallel execution of tasks with synchronized progress output.
// It shows a title line followed by a line per task with live spinner updates.
// Tasks are ordered by start time, with pending tasks shown at the bottom.
//
// In TTY environments (interactive terminals), it uses ANSI escape codes to
// update lines in place with animated spinners.
//
// In non-TTY environments (CI, pipes), it uses a simpler output that only
// prints state changes (started, completed, failed) to avoid log spam.
//
// Example TTY output during execution (with InstallingLabels):
//
//	📦 Installing components...
//	⠦ metrics-server installing
//	⠦ flux installing
//	○ argocd pending
//
// Example TTY output during validation (with ValidatingLabels):
//
//	✅ Validating kustomizations...
//	⠦ apps validating
//	✔ base validated
//	○ cluster pending
//
// Example CI output:
//
//	📦 Installing components...
//	► metrics-server started
//	► flux started
//	✔ flux installed
//	✔ metrics-server installed
type ProgressGroup struct {
	title  string
	emoji  string
	labels ProgressLabels
	writer io.Writer
	timer  timer.Timer
	clock  Clock
	isTTY  bool // Whether output is a TTY (interactive terminal)

	mu             sync.Mutex
	taskStatus     map[string]taskState
	taskOrder      []string                 // Original task order
	taskStartOrder []string                 // Order tasks started running (for display)
	taskStartTime  map[string]time.Time     // Per-task start times
	taskDuration   map[string]time.Duration // Per-task elapsed durations
	spinnerIdx     int
	stopSpinner    chan struct{}
	spinnerDone    chan struct{}
	linesDrawn     int // Number of lines currently drawn (for cursor movement)

	// Compact display mode fields
	maxVisible      int  // Max task lines to show at once (0 = show all)
	concurrency     int  // Max parallel goroutines (0 = unlimited)
	completedCount  int  // Number of completed tasks (for summary line)
	failedCount     int  // Number of failed tasks (for summary line)
	continueOnError bool // Don't cancel other tasks when one fails
	termWidth       int  // Terminal width in columns (0 = non-TTY or unknown)
}

// taskState represents the current state of a task.
type taskState int

const (
	taskPending taskState = iota
	taskRunning
	taskComplete
	taskFailed
)

const (
	// spinnerTickInterval is the interval between spinner animation frames.
	spinnerTickInterval = 100 * time.Millisecond
)

// getSpinnerFrames returns the spinner animation frames.
// Using a function instead of a global variable satisfies gochecknoglobals.
func getSpinnerFrames() []string {
	return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
}

// Clock provides the current time for per-task duration tracking.
// This enables deterministic testing by injecting a fake clock.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

type realClock struct{}

func (realClock) Now() time.Time                  { return time.Now() }
func (realClock) Since(t time.Time) time.Duration { return time.Since(t) }

// ProgressOption is a functional option for configuring a ProgressGroup.
type ProgressOption func(*ProgressGroup)

// WithLabels sets custom labels for task states.
func WithLabels(labels ProgressLabels) ProgressOption {
	return func(pg *ProgressGroup) {
		pg.labels = labels
	}
}

// WithTimer sets the timer for duration tracking.
func WithTimer(tmr timer.Timer) ProgressOption {
	return func(pg *ProgressGroup) {
		pg.timer = tmr
	}
}

// WithClock sets the clock used for per-task duration tracking.
// If not set, the real system clock is used. A nil clock is ignored.
func WithClock(clock Clock) ProgressOption {
	return func(pg *ProgressGroup) {
		if clock == nil {
			return
		}

		pg.clock = clock
	}
}

// WithMaxVisible sets the maximum number of task lines shown at once.
// When set, a compact display mode is used: a summary line at the top
// shows completed/failed counts, up to maxVisible active tasks are shown,
// and a remaining count is shown at the bottom.
// Default 0 means show all tasks (backward compatible).
func WithMaxVisible(n int) ProgressOption {
	return func(pg *ProgressGroup) {
		pg.maxVisible = n
	}
}

// WithConcurrency limits the number of tasks running in parallel.
// Default 0 means unlimited concurrency.
func WithConcurrency(n int) ProgressOption {
	return func(pg *ProgressGroup) {
		pg.concurrency = n
	}
}

// WithContinueOnError makes the group run all tasks even when some fail.
// Without this option, the first failure cancels remaining tasks.
// When set, all errors are collected and returned as a combined error.
func WithContinueOnError() ProgressOption {
	return func(pg *ProgressGroup) {
		pg.continueOnError = true
	}
}

// NewProgressGroup creates a new ProgressGroup for parallel task execution.
// title: The title shown during execution (e.g., "Installing components")
// emoji: Optional emoji for the title (defaults to ►)
// writer: Output writer (defaults to os.Stdout if nil)
// opts: Optional configuration options (WithLabels, WithTimer).
func NewProgressGroup(
	title, emoji string,
	writer io.Writer,
	opts ...ProgressOption,
) *ProgressGroup {
	if writer == nil {
		writer = os.Stdout
	}

	if emoji == "" {
		emoji = "►"
	}

	// Detect if we're outputting to a TTY and get terminal width
	isTTY := false
	termWidth := 0

	if file, ok := writer.(*os.File); ok {
		fd := int(file.Fd()) //nolint:gosec // G115: safe fd conversion
		isTTY = term.IsTerminal(fd)

		if isTTY {
			if w, _, err := term.GetSize(fd); err == nil {
				termWidth = w
			}
		}
	}

	progressGroup := &ProgressGroup{
		title:          title,
		emoji:          emoji,
		labels:         DefaultLabels(),
		writer:         writer,
		clock:          realClock{},
		isTTY:          isTTY,
		termWidth:      termWidth,
		taskStatus:     make(map[string]taskState),
		taskOrder:      make([]string, 0),
		taskStartOrder: make([]string, 0),
		taskStartTime:  make(map[string]time.Time),
		taskDuration:   make(map[string]time.Duration),
		stopSpinner:    make(chan struct{}),
		spinnerDone:    make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		opt(progressGroup)
	}

	return progressGroup
}

// Run executes all tasks in parallel with live progress updates.
// Returns an error if any task fails.
func (pg *ProgressGroup) Run(ctx context.Context, tasks ...ProgressTask) error {
	if len(tasks) == 0 {
		return nil
	}

	// Initialize task status and order
	for _, task := range tasks {
		pg.taskStatus[task.Name] = taskPending
		pg.taskOrder = append(pg.taskOrder, task.Name)
	}

	// Reset timer for this phase
	if pg.timer != nil {
		pg.timer.NewStage()
	}

	// Print title (StageSeparatingWriter handles leading newlines automatically)
	_, _ = fmt.Fprintf(pg.writer, "%s %s...\n", pg.emoji, pg.title)

	// Use different modes for TTY vs non-TTY
	if pg.isTTY {
		return pg.runInteractive(ctx, tasks)
	}

	return pg.runCI(ctx, tasks)
}

// runInteractive runs tasks with animated spinner output for interactive terminals.
func (pg *ProgressGroup) runInteractive(ctx context.Context, tasks []ProgressTask) error {
	// Print initial state (all pending)
	pg.printAllLines()

	// Start spinner animation
	go pg.runSpinner()

	var err error

	if pg.continueOnError {
		err = pg.runAllTasks(ctx, tasks)
	} else {
		err = pg.runFailFastTasks(ctx, tasks)
	}

	// Stop spinner and wait for it to finish
	close(pg.stopSpinner)
	<-pg.spinnerDone

	// Final redraw to show completed state
	pg.redrawAllLines()

	// Print timing if available
	if err == nil && pg.timer != nil {
		pg.printTiming()
	}

	if err != nil {
		return fmt.Errorf("parallel execution: %w", err)
	}

	return nil
}

// runFailFastTasks runs tasks with errgroup.WithContext — first failure cancels the rest.
func (pg *ProgressGroup) runFailFastTasks(ctx context.Context, tasks []ProgressTask) error {
	group, groupCtx := errgroup.WithContext(ctx)

	pg.applyGroupLimits(group)

	for _, task := range tasks {
		group.Go(func() error {
			pg.setTaskState(task.Name, taskRunning)

			taskErr := task.Fn(groupCtx)
			if taskErr != nil {
				pg.setTaskState(task.Name, taskFailed)

				return fmt.Errorf("%s: %w", task.Name, taskErr)
			}

			pg.setTaskState(task.Name, taskComplete)

			return nil
		})
	}

	return group.Wait()
}

// runAllTasks runs all tasks without cancelling on failure. Errors are collected
// and returned as a combined error so the caller sees every failure.
func (pg *ProgressGroup) runAllTasks(ctx context.Context, tasks []ProgressTask) error {
	var group errgroup.Group

	pg.applyGroupLimits(&group)

	var (
		errMu   sync.Mutex
		allErrs []error
	)

	for _, task := range tasks {
		group.Go(func() error {
			pg.setTaskState(task.Name, taskRunning)

			taskErr := task.Fn(ctx)
			if taskErr != nil {
				pg.setTaskState(task.Name, taskFailed)

				errMu.Lock()
				allErrs = append(allErrs, fmt.Errorf("%s: %w", task.Name, taskErr))
				errMu.Unlock()

				return nil // Don't cancel other tasks
			}

			pg.setTaskState(task.Name, taskComplete)

			return nil
		})
	}

	_ = group.Wait()

	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}

	return nil
}

// applyGroupLimits sets the concurrency limit on an errgroup.
func (pg *ProgressGroup) applyGroupLimits(group interface{ SetLimit(n int) }) {
	if pg.concurrency > 0 {
		group.SetLimit(pg.concurrency)
	} else if pg.maxVisible > 0 {
		group.SetLimit(pg.maxVisible)
	}
}

// runCI runs tasks with simple line-based output for CI environments.
// Only prints when tasks start or complete (no spinner animation).
func (pg *ProgressGroup) runCI(ctx context.Context, tasks []ProgressTask) error {
	var taskCtx context.Context

	var group *errgroup.Group

	var (
		errMu   sync.Mutex
		allErrs []error
	)

	if pg.continueOnError {
		group = &errgroup.Group{}
		taskCtx = ctx
	} else {
		var gCtx context.Context
		group, gCtx = errgroup.WithContext(ctx)
		taskCtx = gCtx
	}

	pg.applyGroupLimits(group)

	for _, task := range tasks {
		group.Go(func() error {
			pg.setTaskState(task.Name, taskRunning)

			pg.mu.Lock()
			_, _ = fmt.Fprintf(pg.writer, "► %s %s\n", task.Name, pg.labels.Running)
			pg.mu.Unlock()

			taskErr := task.Fn(taskCtx)
			if taskErr != nil {
				pg.setTaskState(task.Name, taskFailed)

				pg.mu.Lock()
				_, _ = fcolor.New(fcolor.FgRed).Fprintf(pg.writer, "✗ %s failed\n", task.Name)
				pg.mu.Unlock()

				if pg.continueOnError {
					errMu.Lock()
					allErrs = append(allErrs, fmt.Errorf("%s: %w", task.Name, taskErr))
					errMu.Unlock()

					return nil
				}

				return fmt.Errorf("%s: %w", task.Name, taskErr)
			}

			pg.setTaskState(task.Name, taskComplete)

			pg.mu.Lock()
			_, _ = fmt.Fprintln(pg.writer, pg.formatTaskLine(task.Name, taskComplete))
			pg.mu.Unlock()

			return nil
		})
	}

	err := group.Wait()

	if pg.continueOnError && len(allErrs) > 0 {
		err = errors.Join(allErrs...)
	}

	// Print timing if available
	if err == nil && pg.timer != nil {
		pg.printTiming()
	}

	if err != nil {
		return fmt.Errorf("parallel execution: %w", err)
	}

	return nil
}

// printTiming prints the timing information.
func (pg *ProgressGroup) printTiming() {
	total, stage := pg.timer.GetTiming()
	successColor := fcolor.New(fcolor.FgGreen)
	_, _ = successColor.Fprintf(pg.writer, "⏲ current: %s\n", stage.String())
	_, _ = successColor.Fprintf(pg.writer, "  total:  %s\n", total.String())
}

// setTaskState safely updates a task's state and tracks start order and timing.
func (pg *ProgressGroup) setTaskState(name string, state taskState) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	// Track when tasks start running (for display order and timing)
	if state == taskRunning && pg.taskStatus[name] == taskPending {
		pg.taskStartOrder = append(pg.taskStartOrder, name)
		pg.taskStartTime[name] = pg.clock.Now()
	}

	// Record duration when task completes
	if state == taskComplete {
		if startTime, ok := pg.taskStartTime[name]; ok {
			pg.taskDuration[name] = pg.clock.Since(startTime)
		}

		pg.completedCount++
	}

	if state == taskFailed {
		pg.failedCount++
	}

	pg.taskStatus[name] = state
}

// runSpinner animates the spinner until stopped.
func (pg *ProgressGroup) runSpinner() {
	defer close(pg.spinnerDone)

	frames := getSpinnerFrames()
	ticker := time.NewTicker(spinnerTickInterval)

	defer ticker.Stop()

	for {
		select {
		case <-pg.stopSpinner:
			return
		case <-ticker.C:
			pg.mu.Lock()
			pg.spinnerIdx = (pg.spinnerIdx + 1) % len(frames)
			pg.mu.Unlock()
			pg.redrawAllLines()
		}
	}
}

// printAllLines prints task lines (initial draw).
// In compact mode, only the visible window is printed.
func (pg *ProgressGroup) printAllLines() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if pg.maxVisible > 0 {
		pg.printCompactLines()

		return
	}

	for _, name := range pg.taskOrder {
		state := pg.taskStatus[name]
		line := pg.formatTaskLine(name, state)
		_, _ = fmt.Fprintln(pg.writer, line)
	}

	pg.linesDrawn = len(pg.taskOrder)
}

// printCompactLines prints the compact window with a fixed height.
// The window is always compactWindowHeight() lines tall, padded with blank
// lines for unused slots. A fixed height ensures linesDrawn never changes
// after the initial draw, making cursor-up positioning robust across redraws.
// Must be called with mutex held.
func (pg *ProgressGroup) printCompactLines() {
	var buf bytes.Buffer

	fixedHeight := pg.compactWindowHeight()
	contentLines := pg.buildCompactWindow(&buf)

	// Pad to fixed height with blank lines
	for i := contentLines; i < fixedHeight; i++ {
		fmt.Fprint(&buf, "\033[K\n")
	}

	_, _ = pg.writer.Write(buf.Bytes())

	pg.linesDrawn = fixedHeight
}

// compactWindowHeight returns the fixed height of the compact window:
// 1 summary line + maxVisible task slots + 1 remaining line.
func (pg *ProgressGroup) compactWindowHeight() int {
	return pg.maxVisible + 2
}

// buildCompactWindow builds the compact display lines into a buffer and returns
// the number of lines written. Must be called with mutex held.
// Each line is prefixed with \033[K to clear residual content from longer previous lines.
func (pg *ProgressGroup) buildCompactWindow(buf *bytes.Buffer) int {
	lines := 0

	// Summary line: show completed/failed counts (only after first completion)
	if pg.completedCount > 0 || pg.failedCount > 0 {
		fmt.Fprint(buf, "\033[K")
		pg.writeSummaryLine(buf)
		lines++
	}

	// Visible task lines: running tasks first, then failed, then recently completed
	visibleTasks := pg.getVisibleTasks()
	for _, name := range visibleTasks {
		state := pg.taskStatus[name]
		line := pg.formatTaskLine(name, state)
		fmt.Fprintf(buf, "\033[K%s\n", line)
		lines++
	}

	// Remaining count at bottom
	pendingCount := pg.countPending()
	if pendingCount > 0 {
		fmt.Fprintf(buf, "\033[K%s\n", fcolor.New(fcolor.FgHiBlack).Sprintf("○ %d remaining", pendingCount))
		lines++
	}

	return lines
}

// writeSummaryLine writes the summary line with completed/failed counts.
func (pg *ProgressGroup) writeSummaryLine(buf *bytes.Buffer) {
	parts := make([]string, 0, 2)

	if pg.completedCount > 0 {
		parts = append(parts,
			fcolor.New(fcolor.FgGreen).Sprintf("✔ %d %s", pg.completedCount, pg.labels.Completed))
	}

	if pg.failedCount > 0 {
		parts = append(parts,
			fcolor.New(fcolor.FgRed).Sprintf("✗ %d failed", pg.failedCount))
	}

	for i, part := range parts {
		if i > 0 {
			fmt.Fprint(buf, "  ")
		}

		fmt.Fprint(buf, part)
	}

	fmt.Fprintln(buf)
}

// getVisibleTasks returns up to maxVisible tasks that should be shown in the compact window.
// Priority: running tasks first, then failed tasks, then most recently completed tasks.
// Must be called with mutex held.
func (pg *ProgressGroup) getVisibleTasks() []string {
	var running, failed, completed []string

	// Walk start order to categorize tasks
	for _, name := range pg.taskStartOrder {
		state := pg.taskStatus[name]

		switch state {
		case taskRunning:
			running = append(running, name)
		case taskFailed:
			failed = append(failed, name)
		case taskComplete:
			completed = append(completed, name)
		}
	}

	result := make([]string, 0, pg.maxVisible)

	// Running tasks first (in-progress work)
	result = append(result, running...)

	// Then failed tasks (most important to surface)
	slots := pg.maxVisible - len(result)
	if slots > 0 && len(failed) > 0 {
		end := min(slots, len(failed))
		result = append(result, failed[len(failed)-end:]...)
	}

	// Fill remaining slots with most recently completed
	slots = pg.maxVisible - len(result)
	if slots > 0 && len(completed) > 0 {
		start := len(completed) - slots
		if start < 0 {
			start = 0
		}

		result = append(result, completed[start:]...)
	}

	// Cap to maxVisible
	if len(result) > pg.maxVisible {
		result = result[:pg.maxVisible]
	}

	return result
}

// countPending returns the number of pending tasks. Must be called with mutex held.
func (pg *ProgressGroup) countPending() int {
	count := 0

	for _, name := range pg.taskOrder {
		if pg.taskStatus[name] == taskPending {
			count++
		}
	}

	return count
}

// redrawAllLines moves cursor up and redraws all task lines.
// Tasks are ordered: started tasks first (in start order), then pending tasks.
// All ANSI escape sequences are buffered and written atomically to prevent corruption.
func (pg *ProgressGroup) redrawAllLines() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if pg.linesDrawn == 0 {
		return
	}

	var buf bytes.Buffer

	// Move cursor up N lines
	fmt.Fprintf(&buf, "\033[%dA", pg.linesDrawn)

	if pg.maxVisible > 0 {
		// Compact mode: rebuild content and pad to fixed height.
		// linesDrawn stays constant — never changes after initial draw.
		contentLines := pg.buildCompactWindow(&buf)

		for i := contentLines; i < pg.linesDrawn; i++ {
			fmt.Fprint(&buf, "\033[K\n")
		}
	} else {
		// Standard mode: redraw all tasks in display order
		displayOrder := pg.getDisplayOrder()

		for _, name := range displayOrder {
			state := pg.taskStatus[name]
			line := pg.formatTaskLine(name, state)
			// Clear line and print new content
			fmt.Fprint(&buf, "\033[K")
			fmt.Fprintln(&buf, line)
		}
	}

	// Flush entire buffer atomically
	_, _ = pg.writer.Write(buf.Bytes())
}

// getDisplayOrder returns tasks ordered by start time, with pending tasks at the end.
// Must be called with mutex held.
func (pg *ProgressGroup) getDisplayOrder() []string {
	// Create a set of started tasks for quick lookup
	startedSet := make(map[string]bool, len(pg.taskStartOrder))
	for _, name := range pg.taskStartOrder {
		startedSet[name] = true
	}

	// Build result: started tasks first (in start order), then pending
	result := make([]string, 0, len(pg.taskOrder))
	result = append(result, pg.taskStartOrder...)

	// Add pending tasks (maintain original order among pending)
	for _, name := range pg.taskOrder {
		if !startedSet[name] {
			result = append(result, name)
		}
	}

	return result
}

// formatTaskLine formats a task's status line for display.
// Names are truncated to prevent line wrapping in TTY mode.
func (pg *ProgressGroup) formatTaskLine(name string, state taskState) string {
	name = pg.fitName(name, state)
	frames := getSpinnerFrames()

	switch state {
	case taskPending:
		return fcolor.New(fcolor.FgHiBlack).Sprintf("○ %s %s", name, pg.labels.Pending)
	case taskRunning:
		spinner := frames[pg.spinnerIdx]

		return fcolor.New(fcolor.FgCyan).Sprintf("%s %s %s", spinner, name, pg.labels.Running)
	case taskComplete:
		if pg.timer != nil {
			if d, ok := pg.taskDuration[name]; ok {
				return fcolor.New(fcolor.FgGreen).
					Sprintf("✔ %s %s [%s]", name, pg.labels.Completed, d)
			}
		}

		return fcolor.New(fcolor.FgGreen).Sprintf("✔ %s %s", name, pg.labels.Completed)
	case taskFailed:
		return fcolor.New(fcolor.FgRed).Sprintf("✗ %s failed", name)
	default:
		return fmt.Sprintf("? %s unknown", name)
	}
}

// fitName truncates long names to prevent terminal line wrapping in TTY mode.
// Uses middle truncation (beginning…end) to preserve directory context and filename.
// Returns the name unchanged in non-TTY mode or when the name already fits.
func (pg *ProgressGroup) fitName(name string, state taskState) string {
	if pg.termWidth <= 0 {
		return name
	}

	// Calculate display columns used by non-name parts: icon(1) + space + name + space + label
	var suffixLen int

	switch state {
	case taskPending:
		suffixLen = len(pg.labels.Pending)
	case taskRunning:
		suffixLen = len(pg.labels.Running)
	case taskComplete:
		suffixLen = len(pg.labels.Completed)

		if pg.timer != nil {
			suffixLen += 12 // " [XXXms]" approximate
		}
	case taskFailed:
		suffixLen = 6 // "failed"
	}

	// icon(1 col) + space(1) + name + space(1) + suffix
	overhead := 3 + suffixLen
	maxNameLen := pg.termWidth - overhead - 1 // -1 safety margin

	const minNameLen = 10
	if maxNameLen < minNameLen {
		maxNameLen = minNameLen
	}

	if len(name) <= maxNameLen {
		return name
	}

	// Middle truncation: show beginning and end for best context
	firstLen := (maxNameLen - 1) / 2
	lastLen := maxNameLen - 1 - firstLen

	return name[:firstLen] + "…" + name[len(name)-lastLen:]
}

package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/timer"
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

// ReconcilingLabels returns labels suitable for reconciliation tasks.
func ReconcilingLabels() ProgressLabels {
	return ProgressLabels{
		Pending:   "pending",
		Running:   "reconciling",
		Completed: "reconciled",
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
	maxVisible      int             // Max task lines to show at once (0 = show all)
	concurrency     int             // Max parallel goroutines (0 = unlimited)
	completedCount  int             // Number of completed tasks (for summary line)
	failedCount     int             // Number of failed tasks (for summary line)
	continueOnError bool            // Don't cancel other tasks when one fails
	appendOnly      bool            // Force append-only output (no ANSI redraws, no starting lines)
	termWidth       int             // Terminal width in columns (0 = non-TTY or unknown)
	countLabel      string          // Label for counts (e.g., "kustomizations", "files")
	emitted         map[string]bool // tasks whose completion has been permanently printed (streaming mode)
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
	// streamingPendingPreview is the number of pending tasks shown below
	// running tasks in streaming interactive mode.
	streamingPendingPreview = 3
	// compactWindowExtraLines is the number of non-task lines in the compact window
	// (1 summary line + 1 remaining count line).
	compactWindowExtraLines = 2
	// summaryPartsInitCap is the initial capacity of the summary parts slice
	// (at most 2 parts: completed and failed).
	summaryPartsInitCap = 2
	// taskLineNonNameCols is the number of columns used by non-name parts of a task line
	// (icon + space + space + suffix = 3 cols total, excluding suffix length).
	taskLineNonNameCols = 3
	// truncationHalvingDivisor halves the max name length to compute the first portion
	// when middle-truncating long names.
	truncationHalvingDivisor = 2
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

// WithConcurrency limits the number of tasks running in parallel.
// Default 0 means unlimited concurrency.
func WithConcurrency(n int) ProgressOption {
	return func(pg *ProgressGroup) {
		pg.concurrency = n
	}
}

// WithCountLabel sets a label used in summary lines to describe what is being counted.
// For example, WithCountLabel("kustomizations") produces "✔ 62 kustomizations validated".
// Without this, the summary shows just "✔ 62 validated".
func WithCountLabel(label string) ProgressOption {
	return func(pg *ProgressGroup) {
		pg.countLabel = label
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

// WithAppendOnly forces append-only output regardless of TTY detection.
// Completed tasks are emitted as permanent lines rather than being updated
// in-place. In TTY mode, the output still uses ANSI cursor movement and
// spinners for a live zone showing running and pending tasks.
// Use with WithConcurrency to control parallelism.
func WithAppendOnly() ProgressOption {
	return func(pg *ProgressGroup) {
		pg.appendOnly = true
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
			w, _, err := term.GetSize(fd)
			if err == nil {
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
		emitted:        make(map[string]bool),
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
	if pg.title != "" {
		_, _ = fmt.Fprintf(pg.writer, "%s %s...\n", pg.emoji, pg.title)
	}

	// Use different modes for TTY vs non-TTY
	if pg.appendOnly && pg.isTTY {
		return pg.runStreamingInteractive(ctx, tasks)
	}

	if pg.isTTY {
		return pg.runInteractive(ctx, tasks)
	}

	return pg.runCI(ctx, tasks)
}

// runInteractive runs tasks with animated spinner output for interactive terminals.
func (pg *ProgressGroup) runInteractive(ctx context.Context, tasks []ProgressTask) error {
	pg.printAllLines()

	return pg.runWithSpinner(ctx, tasks, pg.redrawAllLines)
}

// runStreamingInteractive runs tasks with a hybrid display: completed lines
// scroll up permanently while a small live zone at the bottom shows running
// tasks with spinners and a preview of the next pending tasks.
func (pg *ProgressGroup) runStreamingInteractive(ctx context.Context, tasks []ProgressTask) error {
	pg.drawStreamingZone()

	return pg.runWithSpinner(ctx, tasks, pg.finalizeStreamingOutput)
}

// runWithSpinner orchestrates the common spinner lifecycle shared by interactive modes:
// start spinner → run tasks → stop spinner → finalize display → print summary/timing.
func (pg *ProgressGroup) runWithSpinner(
	ctx context.Context,
	tasks []ProgressTask,
	finalize func(),
) error {
	go pg.runSpinner()

	err := pg.executeTasks(ctx, tasks)

	close(pg.stopSpinner)
	<-pg.spinnerDone

	finalize()

	pg.printFinalSummary()

	if err == nil && pg.timer != nil {
		pg.printTiming()
	}

	return err
}

// executeTasks runs tasks using the appropriate strategy based on continueOnError.
func (pg *ProgressGroup) executeTasks(ctx context.Context, tasks []ProgressTask) error {
	if pg.continueOnError {
		return pg.runAllTasks(ctx, tasks)
	}

	return pg.runFailFastTasks(ctx, tasks)
}

// drawStreamingZone draws the initial live zone showing pending tasks.
// The zone covers at most concurrency + streamingPendingPreview lines.
// When concurrency is unset (0), the zone is capped to streamingPendingPreview lines
// to avoid flooding the terminal.
func (pg *ProgressGroup) drawStreamingZone() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	maxInitial := streamingPendingPreview
	if pg.concurrency > 0 {
		maxInitial = pg.concurrency + streamingPendingPreview
	}

	var buf bytes.Buffer

	shown := 0

	for _, name := range pg.taskOrder {
		if shown >= maxInitial {
			break
		}

		line := pg.formatTaskLine(name, pg.taskStatus[name])
		fmt.Fprintf(&buf, "%s\n", line)

		shown++
	}

	_, _ = pg.writer.Write(buf.Bytes())

	pg.linesDrawn = shown
}

// emitCompleted writes permanent lines for newly completed/failed tasks into buf.
// Returns the number of lines emitted. Must be called with mutex held.
func (pg *ProgressGroup) emitCompleted(buf *bytes.Buffer) int {
	count := 0

	for _, name := range pg.taskStartOrder {
		state := pg.taskStatus[name]
		if (state == taskComplete || state == taskFailed) && !pg.emitted[name] {
			fmt.Fprintf(buf, "\033[K%s\n", pg.formatTaskLine(name, state))

			pg.emitted[name] = true
			count++
		}
	}

	return count
}

// redrawStreamingZone redraws only the live zone at the bottom of the output.
// Newly completed tasks are emitted as permanent lines above the zone.
// The live zone shows currently running tasks plus up to streamingPendingPreview
// pending tasks, updated in place via ANSI cursor movement.
func (pg *ProgressGroup) redrawStreamingZone() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if pg.linesDrawn == 0 && len(pg.emitted) == len(pg.taskOrder) {
		return // nothing to draw
	}

	// Move cursor to start of live zone
	if pg.linesDrawn > 0 {
		_, _ = fmt.Fprintf(pg.writer, "\033[%dA", pg.linesDrawn)
	}

	var buf bytes.Buffer

	emittedCount := pg.emitCompleted(&buf)

	// Build live zone: running tasks (in start order)
	liveLines := 0

	for _, name := range pg.taskStartOrder {
		if pg.taskStatus[name] == taskRunning {
			fmt.Fprintf(&buf, "\033[K%s\n", pg.formatTaskLine(name, taskRunning))

			liveLines++
		}
	}

	// Append up to streamingPendingPreview pending tasks
	pendingShown := 0

	for _, name := range pg.taskOrder {
		if pg.taskStatus[name] == taskPending && pendingShown < streamingPendingPreview {
			fmt.Fprintf(&buf, "\033[K%s\n", pg.formatTaskLine(name, taskPending))

			liveLines++
			pendingShown++
		}
	}

	// Clear excess lines from previous draw
	for i := emittedCount + liveLines; i < pg.linesDrawn; i++ {
		fmt.Fprint(&buf, "\033[K\n")
	}

	_, _ = pg.writer.Write(buf.Bytes())

	pg.linesDrawn = liveLines // only the live zone is rewritable next time
}

// finalizeStreamingOutput emits any remaining completed/failed lines and
// clears the live zone. Called after spinner stops and all tasks are done.
func (pg *ProgressGroup) finalizeStreamingOutput() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if pg.linesDrawn > 0 {
		_, _ = fmt.Fprintf(pg.writer, "\033[%dA", pg.linesDrawn)
	}

	var buf bytes.Buffer

	emittedCount := pg.emitCompleted(&buf)

	// Clear leftover live zone lines and reposition cursor
	excess := pg.linesDrawn - emittedCount
	for range excess {
		fmt.Fprint(&buf, "\033[K\n")
	}

	if excess > 0 {
		fmt.Fprintf(&buf, "\033[%dA", excess)
	}

	_, _ = pg.writer.Write(buf.Bytes())

	pg.linesDrawn = 0
}

// runTask executes a single task with state tracking: pending→running→complete/failed.
// Returns a labeled error on failure.
func (pg *ProgressGroup) runTask(ctx context.Context, task ProgressTask) error {
	pg.setTaskState(task.Name, taskRunning)

	err := task.Fn(ctx)
	if err != nil {
		pg.setTaskState(task.Name, taskFailed)

		return fmt.Errorf("%s: %w", task.Name, err)
	}

	pg.setTaskState(task.Name, taskComplete)

	return nil
}

// runFailFastTasks runs tasks with errgroup.WithContext — first failure cancels the rest.
func (pg *ProgressGroup) runFailFastTasks(ctx context.Context, tasks []ProgressTask) error {
	group, groupCtx := errgroup.WithContext(ctx)

	pg.applyGroupLimits(group)

	for _, task := range tasks {
		group.Go(func() error {
			return pg.runTask(groupCtx, task)
		})
	}

	return group.Wait() //nolint:wrapcheck // runTask already labels errors with the task name
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
			err := pg.runTask(ctx, task)
			if err != nil {
				errMu.Lock()

				allErrs = append(allErrs, err)
				errMu.Unlock()
			}

			return nil // Don't cancel other tasks
		})
	}

	_ = group.Wait()

	return errors.Join(allErrs...)
}

// applyGroupLimits sets the concurrency limit on an errgroup.
func (pg *ProgressGroup) applyGroupLimits(group interface{ SetLimit(n int) }) {
	if pg.concurrency > 0 {
		group.SetLimit(pg.concurrency)
	} else if pg.maxVisible > 0 {
		group.SetLimit(pg.maxVisible)
	}
}

// runCIWorker returns a goroutine function that executes a single task in CI mode.
// It updates task state and records errors when continueOnError is set.
func (pg *ProgressGroup) runCIWorker(
	ctx context.Context,
	task ProgressTask,
	errMu *sync.Mutex,
	allErrs *[]error,
) func() error {
	return func() error {
		pg.setTaskState(task.Name, taskRunning)

		if !pg.appendOnly {
			pg.mu.Lock()
			_, _ = fmt.Fprintf(pg.writer, "► %s %s\n", task.Name, pg.labels.Running)
			pg.mu.Unlock()
		}

		taskErr := task.Fn(ctx)
		if taskErr != nil {
			pg.setTaskState(task.Name, taskFailed)

			pg.mu.Lock()
			_, _ = fcolor.New(fcolor.FgRed).Fprintf(pg.writer, "✗ %s failed\n", task.Name)
			pg.mu.Unlock()

			if pg.continueOnError {
				errMu.Lock()

				*allErrs = append(*allErrs, fmt.Errorf("%s: %w", task.Name, taskErr))
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
		group.Go(pg.runCIWorker(taskCtx, task, &errMu, &allErrs))
	}

	err := group.Wait()

	if pg.continueOnError && len(allErrs) > 0 {
		err = errors.Join(allErrs...)
	}

	// Print final summary below task lines
	pg.printFinalSummary()

	// Print timing if available
	if err == nil && pg.timer != nil {
		pg.printTiming()
	}

	return err
}

// printFinalSummary prints a one-line summary after all tasks complete.
// Example: "✔ 62 kustomizations validated" or "✔ 60 validated  ✗ 2 failed".
func (pg *ProgressGroup) printFinalSummary() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if summary := pg.formatSummary(); summary != "" {
		_, _ = fmt.Fprintln(pg.writer, summary)
	}
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

			if pg.appendOnly {
				pg.redrawStreamingZone()
			} else {
				pg.redrawAllLines()
			}
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
	return pg.maxVisible + compactWindowExtraLines
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
		fmt.Fprintf(
			buf,
			"\033[K%s\n",
			fcolor.New(fcolor.FgHiBlack).Sprintf("○ %d remaining", pendingCount),
		)

		lines++
	}

	return lines
}

// writeSummaryLine writes the summary line with completed/failed counts.
// Must be called with mutex held.
func (pg *ProgressGroup) writeSummaryLine(buf *bytes.Buffer) {
	if summary := pg.formatSummary(); summary != "" {
		fmt.Fprintln(buf, summary)
	}
}

// formatSummary builds the summary string with completed/failed counts.
// Returns empty string when there are no completed or failed tasks.
// Must be called with mutex held.
func (pg *ProgressGroup) formatSummary() string {
	parts := make([]string, 0, summaryPartsInitCap)

	if pg.completedCount > 0 {
		if pg.countLabel != "" {
			parts = append(
				parts,
				fcolor.New(fcolor.FgGreen).
					Sprintf("✔ %d %s %s", pg.completedCount, pg.countLabel, pg.labels.Completed),
			)
		} else {
			parts = append(
				parts,
				fcolor.New(fcolor.FgGreen).
					Sprintf("✔ %d %s", pg.completedCount, pg.labels.Completed),
			)
		}
	}

	if pg.failedCount > 0 {
		parts = append(parts,
			fcolor.New(fcolor.FgRed).Sprintf("✗ %d failed", pg.failedCount))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "  ")
}

// appendSlots appends items (from the tail) to fill remaining slots up to limit.
// Does nothing when result is already at or above limit or items is empty.
func appendSlots(result, items []string, limit int) []string {
	slots := limit - len(result)
	if slots <= 0 || len(items) == 0 {
		return result
	}

	start := max(len(items)-slots, 0)

	return append(result, items[start:]...)
}

// getVisibleTasks returns up to maxVisible tasks that should be shown in the compact window.
// Priority: running tasks first, then failed tasks, then most recently completed tasks.
// Must be called with mutex held.
func (pg *ProgressGroup) getVisibleTasks() []string {
	var running, failed, completed []string

	// Walk start order to categorize tasks
	for _, name := range pg.taskStartOrder {
		switch pg.taskStatus[name] {
		case taskRunning:
			running = append(running, name)
		case taskFailed:
			failed = append(failed, name)
		case taskComplete:
			completed = append(completed, name)
		case taskPending:
			// pending tasks are counted separately via countPending; skip here
		}
	}

	result := make([]string, 0, pg.maxVisible)

	// Running tasks first (in-progress work)
	result = append(result, running...)

	// Then failed tasks (most important to surface), then most recently completed
	result = appendSlots(result, failed, pg.maxVisible)
	result = appendSlots(result, completed, pg.maxVisible)

	// Cap to maxVisible (protects against running > maxVisible)
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
	overhead := taskLineNonNameCols + suffixLen
	maxNameLen := pg.termWidth - overhead - 1 // -1 safety margin

	const minNameLen = 10
	if maxNameLen < minNameLen {
		maxNameLen = minNameLen
	}

	if len(name) <= maxNameLen {
		return name
	}

	// Middle truncation: show beginning and end for best context
	firstLen := (maxNameLen - 1) / truncationHalvingDivisor
	lastLen := maxNameLen - 1 - firstLen

	return name[:firstLen] + "…" + name[len(name)-lastLen:]
}

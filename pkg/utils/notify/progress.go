package notify

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
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
	isTTY  bool // Whether output is a TTY (interactive terminal)

	mu             sync.Mutex
	taskStatus     map[string]taskState
	taskOrder      []string // Original task order
	taskStartOrder []string // Order tasks started running (for display)
	spinnerIdx     int
	stopSpinner    chan struct{}
	spinnerDone    chan struct{}
	linesDrawn     int // Number of lines currently drawn (for cursor movement)
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

	// Detect if we're outputting to a TTY
	isTTY := false
	if file, ok := writer.(*os.File); ok {
		isTTY = term.IsTerminal(int(file.Fd()))
	}

	progressGroup := &ProgressGroup{
		title:          title,
		emoji:          emoji,
		labels:         DefaultLabels(),
		writer:         writer,
		isTTY:          isTTY,
		taskStatus:     make(map[string]taskState),
		taskOrder:      make([]string, 0),
		taskStartOrder: make([]string, 0),
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

	// Execute tasks in parallel
	group, groupCtx := errgroup.WithContext(ctx)

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

	err := group.Wait()

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

// runCI runs tasks with simple line-based output for CI environments.
// Only prints when tasks start or complete (no spinner animation).
func (pg *ProgressGroup) runCI(ctx context.Context, tasks []ProgressTask) error {
	// Execute tasks in parallel
	group, groupCtx := errgroup.WithContext(ctx)

	for _, task := range tasks {
		group.Go(func() error {
			// Print start message
			pg.mu.Lock()
			pg.taskStatus[task.Name] = taskRunning
			_, _ = fmt.Fprintf(pg.writer, "► %s %s\n", task.Name, pg.labels.Running)
			pg.mu.Unlock()

			taskErr := task.Fn(groupCtx)

			pg.mu.Lock()

			if taskErr != nil {
				pg.taskStatus[task.Name] = taskFailed
				_, _ = fcolor.New(fcolor.FgRed).Fprintf(pg.writer, "✗ %s failed\n", task.Name)
			} else {
				pg.taskStatus[task.Name] = taskComplete
				_, _ = fcolor.New(fcolor.FgGreen).Fprintf(pg.writer, "✔ %s %s\n", task.Name, pg.labels.Completed)
			}

			pg.mu.Unlock()

			if taskErr != nil {
				return fmt.Errorf("%s: %w", task.Name, taskErr)
			}

			return nil
		})
	}

	err := group.Wait()

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

// setTaskState safely updates a task's state and tracks start order.
func (pg *ProgressGroup) setTaskState(name string, state taskState) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	// Track when tasks start running (for display order)
	if state == taskRunning && pg.taskStatus[name] == taskPending {
		pg.taskStartOrder = append(pg.taskStartOrder, name)
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

// printAllLines prints all task lines (initial draw).
func (pg *ProgressGroup) printAllLines() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	for _, name := range pg.taskOrder {
		state := pg.taskStatus[name]
		line := pg.formatTaskLine(name, state)
		_, _ = fmt.Fprintln(pg.writer, line)
	}

	pg.linesDrawn = len(pg.taskOrder)
}

// redrawAllLines moves cursor up and redraws all task lines.
// Tasks are ordered: started tasks first (in start order), then pending tasks.
func (pg *ProgressGroup) redrawAllLines() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if pg.linesDrawn == 0 {
		return
	}

	// Move cursor up N lines
	_, _ = fmt.Fprintf(pg.writer, "\033[%dA", pg.linesDrawn)

	// Build display order: started tasks first, then pending
	displayOrder := pg.getDisplayOrder()

	// Redraw each line
	for _, name := range displayOrder {
		state := pg.taskStatus[name]
		line := pg.formatTaskLine(name, state)
		// Clear line and print new content
		_, _ = fmt.Fprint(pg.writer, "\033[K")
		_, _ = fmt.Fprintln(pg.writer, line)
	}
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
func (pg *ProgressGroup) formatTaskLine(name string, state taskState) string {
	frames := getSpinnerFrames()

	switch state {
	case taskPending:
		return fcolor.New(fcolor.FgHiBlack).Sprintf("○ %s %s", name, pg.labels.Pending)
	case taskRunning:
		spinner := frames[pg.spinnerIdx]

		return fcolor.New(fcolor.FgCyan).Sprintf("%s %s %s", spinner, name, pg.labels.Running)
	case taskComplete:
		return fcolor.New(fcolor.FgGreen).Sprintf("✔ %s %s", name, pg.labels.Completed)
	case taskFailed:
		return fcolor.New(fcolor.FgRed).Sprintf("✗ %s failed", name)
	default:
		return fmt.Sprintf("? %s unknown", name)
	}
}

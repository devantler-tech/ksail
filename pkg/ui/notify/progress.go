package notify

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/ui/timer"
	fcolor "github.com/fatih/color"
	"golang.org/x/sync/errgroup"
)

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
// Example output during execution:
//
//	📦 Install components...
//	⠦ installing metrics-server
//	⠦ installing flux
//	○ pending argocd
//
// After completion:
//
//	📦 Install components...
//	✔ metrics-server installed
//	✔ flux installed
//	✔ argocd installed
type ProgressGroup struct {
	title  string
	emoji  string
	writer io.Writer
	timer  timer.Timer

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

// NewProgressGroup creates a new ProgressGroup for parallel task execution.
// title: The title shown during execution (e.g., "Install components")
// emoji: Optional emoji for the title (defaults to 📦)
// writer: Output writer (defaults to os.Stdout if nil)
// tmr: Optional timer for duration tracking.
func NewProgressGroup(title, emoji string, writer io.Writer, tmr timer.Timer) *ProgressGroup {
	if writer == nil {
		writer = os.Stdout
	}

	if emoji == "" {
		emoji = "📦"
	}

	return &ProgressGroup{
		title:          title,
		emoji:          emoji,
		writer:         writer,
		timer:          tmr,
		taskStatus:     make(map[string]taskState),
		taskOrder:      make([]string, 0),
		taskStartOrder: make([]string, 0),
		stopSpinner:    make(chan struct{}),
		spinnerDone:    make(chan struct{}),
	}
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

	// Print title
	_, _ = fmt.Fprintf(pg.writer, "%s %s...\n", pg.emoji, pg.title)

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
		total, stage := pg.timer.GetTiming()
		successColor := fcolor.New(fcolor.FgGreen)
		_, _ = successColor.Fprintf(pg.writer, "⏲ current: %s\n", stage.String())
		_, _ = successColor.Fprintf(pg.writer, "  total:  %s\n", total.String())
	}

	if err != nil {
		return fmt.Errorf("parallel execution: %w", err)
	}

	return nil
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
		return fcolor.New(fcolor.FgHiBlack).Sprintf("○ %s pending", name)
	case taskRunning:
		spinner := frames[pg.spinnerIdx]

		return fcolor.New(fcolor.FgCyan).Sprintf("%s %s installing", spinner, name)
	case taskComplete:
		return fcolor.New(fcolor.FgGreen).Sprintf("✔ %s installed", name)
	case taskFailed:
		return fcolor.New(fcolor.FgRed).Sprintf("✗ %s failed", name)
	default:
		return fmt.Sprintf("? %s unknown", name)
	}
}

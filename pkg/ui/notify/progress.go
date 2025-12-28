package notify

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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
// It shows a unified title, live progress for each task, and a final status message.
type ProgressGroup struct {
	title  string
	emoji  string
	writer io.Writer
	timer  timer.Timer

	mu          sync.Mutex
	taskStatus  map[string]taskState
	spinnerIdx  int
	stopSpinner chan struct{}
	spinnerDone chan struct{}
}

// taskState represents the current state of a task.
type taskState int

const (
	taskPending taskState = iota
	taskRunning
	taskComplete
	taskFailed
)

// spinner characters for animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewProgressGroup creates a new ProgressGroup for parallel task execution.
// title: The title shown during execution (e.g., "Installing components")
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
		title:       title,
		emoji:       emoji,
		writer:      writer,
		timer:       tmr,
		taskStatus:  make(map[string]taskState),
		stopSpinner: make(chan struct{}),
		spinnerDone: make(chan struct{}),
	}
}

// Run executes all tasks in parallel with live progress updates.
// Returns an error if any task fails.
func (pg *ProgressGroup) Run(ctx context.Context, tasks ...ProgressTask) error {
	if len(tasks) == 0 {
		return nil
	}

	// Initialize task status
	taskNames := make([]string, 0, len(tasks))

	for _, task := range tasks {
		pg.taskStatus[task.Name] = taskPending
		taskNames = append(taskNames, task.Name)
	}

	// Reset timer for this phase
	if pg.timer != nil {
		pg.timer.NewStage()
	}

	// Print initial state
	pg.printProgress(taskNames)

	// Start spinner animation
	go pg.runSpinner(taskNames)

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

	// Clear the progress line and print final status
	pg.clearLine()
	pg.printFinalStatus(taskNames, err)

	return err
}

// setTaskState safely updates a task's state.
func (pg *ProgressGroup) setTaskState(name string, state taskState) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	pg.taskStatus[name] = state
}

// getTaskState safely retrieves a task's state.
func (pg *ProgressGroup) getTaskState(name string) taskState {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	return pg.taskStatus[name]
}

// runSpinner animates the spinner until stopped.
func (pg *ProgressGroup) runSpinner(taskNames []string) {
	defer close(pg.spinnerDone)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pg.stopSpinner:
			return
		case <-ticker.C:
			pg.mu.Lock()
			pg.spinnerIdx = (pg.spinnerIdx + 1) % len(spinnerFrames)
			pg.mu.Unlock()
			pg.printProgress(taskNames)
		}
	}
}

// printProgress prints the current progress state.
func (pg *ProgressGroup) printProgress(taskNames []string) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	var parts []string

	for _, name := range taskNames {
		state := pg.taskStatus[name]
		parts = append(parts, pg.formatTaskStatus(name, state))
	}

	// Build the progress line
	statusLine := strings.Join(parts, " ")

	// Clear line and print progress
	pg.clearLineNoLock()
	_, _ = fmt.Fprintf(pg.writer, "%s %s %s", pg.emoji, pg.title, statusLine)
}

// formatTaskStatus formats a single task's status for display.
func (pg *ProgressGroup) formatTaskStatus(name string, state taskState) string {
	switch state {
	case taskPending:
		return fcolor.New(fcolor.FgHiBlack).Sprintf("[%s ○]", name)
	case taskRunning:
		spinner := spinnerFrames[pg.spinnerIdx]

		return fcolor.New(fcolor.FgCyan).Sprintf("[%s %s]", name, spinner)
	case taskComplete:
		return fcolor.New(fcolor.FgGreen).Sprintf("[%s ✔]", name)
	case taskFailed:
		return fcolor.New(fcolor.FgRed).Sprintf("[%s ✗]", name)
	default:
		return fmt.Sprintf("[%s ?]", name)
	}
}

// clearLine clears the current terminal line (with lock).
func (pg *ProgressGroup) clearLine() {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	pg.clearLineNoLock()
}

// clearLineNoLock clears the current terminal line (caller must hold lock).
func (pg *ProgressGroup) clearLineNoLock() {
	// Move cursor to beginning and clear line
	_, _ = fmt.Fprint(pg.writer, "\r\033[K")
}

// printFinalStatus prints the final success or error message.
func (pg *ProgressGroup) printFinalStatus(taskNames []string, err error) {
	if err != nil {
		// Print error status
		WriteMessage(Message{
			Type:    ErrorType,
			Content: fmt.Sprintf("failed to install: %v", err),
			Writer:  pg.writer,
		})

		return
	}

	// Build success message with all component names
	componentList := strings.Join(taskNames, ", ")

	WriteMessage(Message{
		Type:    SuccessType,
		Content: componentList + " installed",
		Timer:   pg.timer,
		Writer:  pg.writer,
	})
}

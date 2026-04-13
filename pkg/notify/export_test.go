package notify

import (
	"bytes"
	"context"
)

//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var (
	StartsWithEmojiForTest = startsWithEmoji
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	TaskPendingForTest = taskPending
	TaskRunningForTest = taskRunning
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	TaskCompleteForTest = taskComplete
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	TaskFailedForTest = taskFailed
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	AppendSlotsForTest      = appendSlots
	GetMessageConfigForTest = getMessageConfig
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	IndentMultilineContentForTest = indentMultilineContent
)

// TaskState is an alias for the unexported taskState type.
//

type TaskState = taskState

// ProgressGroupForTest provides access to ProgressGroup internals for testing.
// It wraps a ProgressGroup to expose unexported methods.
//

type ProgressGroupForTest struct {
	*ProgressGroup
}

// NewProgressGroupForTest creates a test wrapper around a ProgressGroup.
//

func NewProgressGroupForTest(pg *ProgressGroup) *ProgressGroupForTest {
	return &ProgressGroupForTest{ProgressGroup: pg}
}

// FitName exposes fitName for testing.
func (t *ProgressGroupForTest) FitName(name string, state TaskState) string {
	return t.fitName(name, state)
}

// CompactWindowHeight exposes compactWindowHeight for testing.
func (t *ProgressGroupForTest) CompactWindowHeight() int {
	return t.compactWindowHeight()
}

// CountPending exposes countPending for testing.
func (t *ProgressGroupForTest) CountPending() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.countPending()
}

// GetDisplayOrder exposes getDisplayOrder for testing.
func (t *ProgressGroupForTest) GetDisplayOrder() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.getDisplayOrder()
}

// GetVisibleTasks exposes getVisibleTasks for testing.
func (t *ProgressGroupForTest) GetVisibleTasks() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.getVisibleTasks()
}

// FormatTaskLine exposes formatTaskLine for testing.
func (t *ProgressGroupForTest) FormatTaskLine(name string, state TaskState) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.formatTaskLine(name, state)
}

// SetTaskStatusForTest sets a task's status in the internal map.
func (t *ProgressGroupForTest) SetTaskStatusForTest(name string, state TaskState) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.taskStatus[name] = state
}

// AddTaskOrderForTest appends a task name to the task order.
func (t *ProgressGroupForTest) AddTaskOrderForTest(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.taskOrder = append(t.taskOrder, name)
}

// AddTaskStartOrderForTest appends a task name to the start order.
func (t *ProgressGroupForTest) AddTaskStartOrderForTest(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.taskStartOrder = append(t.taskStartOrder, name)
}

// SetMaxVisibleForTest sets maxVisible for testing.
func (t *ProgressGroupForTest) SetMaxVisibleForTest(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.maxVisible = n
}

// SetTermWidthForTest sets termWidth for testing.
func (t *ProgressGroupForTest) SetTermWidthForTest(w int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.termWidth = w
}

// SetCompletedCountForTest sets completedCount for testing.
func (t *ProgressGroupForTest) SetCompletedCountForTest(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.completedCount = n
}

// SetFailedCountForTest sets failedCount for testing.
func (t *ProgressGroupForTest) SetFailedCountForTest(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.failedCount = n
}

// FormatSummaryForTest exposes formatSummary for testing.
func (t *ProgressGroupForTest) FormatSummaryForTest() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.formatSummary()
}

// BuildCompactWindowForTest exposes buildCompactWindow for testing.
// Returns the buffer contents and the number of content lines.
func (t *ProgressGroupForTest) BuildCompactWindowForTest() (string, int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var buf bytes.Buffer

	lines := t.buildCompactWindow(&buf)

	return buf.String(), lines
}

// WriteSummaryLineForTest exposes writeSummaryLine for testing.
func (t *ProgressGroupForTest) WriteSummaryLineForTest() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var buf bytes.Buffer

	t.writeSummaryLine(&buf)

	return buf.String()
}

// EmitCompletedForTest exposes emitCompleted for testing.
func (t *ProgressGroupForTest) EmitCompletedForTest() (string, int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var buf bytes.Buffer

	count := t.emitCompleted(&buf)

	return buf.String(), count
}

// RunTaskForTest exposes runTask for testing.
func (t *ProgressGroupForTest) RunTaskForTest(ctx context.Context, task ProgressTask) error {
	return t.runTask(ctx, task)
}

// ExecuteTasksForTest exposes executeTasks for testing.
func (t *ProgressGroupForTest) ExecuteTasksForTest(
	ctx context.Context,
	tasks []ProgressTask,
) error {
	return t.executeTasks(ctx, tasks)
}

// SetEmittedForTest marks a task as already emitted.
func (t *ProgressGroupForTest) SetEmittedForTest(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.emitted[name] = true
}

// PrintAllLinesForTest exposes printAllLines for testing.
func (t *ProgressGroupForTest) PrintAllLinesForTest() {
	t.printAllLines()
}

// RedrawAllLinesForTest exposes redrawAllLines for testing.
func (t *ProgressGroupForTest) RedrawAllLinesForTest() {
	t.redrawAllLines()
}

// GetLinesDrawnForTest returns the number of lines drawn.
func (t *ProgressGroupForTest) GetLinesDrawnForTest() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.linesDrawn
}

// DrawStreamingZoneForTest exposes drawStreamingZone for testing.
func (t *ProgressGroupForTest) DrawStreamingZoneForTest() {
	t.drawStreamingZone()
}

// RedrawStreamingZoneForTest exposes redrawStreamingZone for testing.
func (t *ProgressGroupForTest) RedrawStreamingZoneForTest() {
	t.redrawStreamingZone()
}

// FinalizeStreamingOutputForTest exposes finalizeStreamingOutput for testing.
func (t *ProgressGroupForTest) FinalizeStreamingOutputForTest() {
	t.finalizeStreamingOutput()
}

// RunSpinnerForTest starts the spinner and returns a stop function.
func (t *ProgressGroupForTest) RunSpinnerForTest() func() {
	go t.runSpinner()

	return func() {
		close(t.stopSpinner)
		<-t.spinnerDone
	}
}

// SetLinesDrawnForTest sets the lines drawn count.
func (t *ProgressGroupForTest) SetLinesDrawnForTest(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.linesDrawn = n
}

// SetAppendOnlyForTest sets appendOnly mode.
func (t *ProgressGroupForTest) SetAppendOnlyForTest(v bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.appendOnly = v
}

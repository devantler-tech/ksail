package notify

import (
	"bytes"
	"context"
)

//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions

// StartsWithEmojiForTest exposes startsWithEmoji for testing.
var StartsWithEmojiForTest = startsWithEmoji

// TaskPendingForTest exposes taskPending for testing.
var TaskPendingForTest = taskPending

// TaskRunningForTest exposes taskRunning for testing.
var TaskRunningForTest = taskRunning

// TaskCompleteForTest exposes taskComplete for testing.
var TaskCompleteForTest = taskComplete

// TaskFailedForTest exposes taskFailed for testing.
var TaskFailedForTest = taskFailed

// AppendSlotsForTest exposes appendSlots for testing.
var AppendSlotsForTest = appendSlots

// GetMessageConfigForTest exposes getMessageConfig for testing.
var GetMessageConfigForTest = getMessageConfig

// IndentMultilineContentForTest exposes indentMultilineContent for testing.
var IndentMultilineContentForTest = indentMultilineContent

// TaskState is an alias for the unexported taskState type.
type TaskState = taskState

// ProgressGroupForTest provides access to ProgressGroup internals for testing.
// It wraps a ProgressGroup to expose unexported methods.
type ProgressGroupForTest struct {
	*ProgressGroup
}

// NewProgressGroupForTest creates a test wrapper around a ProgressGroup.
func NewProgressGroupForTest(pg *ProgressGroup) *ProgressGroupForTest {
	return &ProgressGroupForTest{ProgressGroup: pg}
}

// FitName exposes fitName for testing.
func (t *ProgressGroupForTest) FitName(name string, state TaskState) string {
	return t.ProgressGroup.fitName(name, state)
}

// CompactWindowHeight exposes compactWindowHeight for testing.
func (t *ProgressGroupForTest) CompactWindowHeight() int {
	return t.ProgressGroup.compactWindowHeight()
}

// CountPending exposes countPending for testing.
func (t *ProgressGroupForTest) CountPending() int {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	return t.ProgressGroup.countPending()
}

// GetDisplayOrder exposes getDisplayOrder for testing.
func (t *ProgressGroupForTest) GetDisplayOrder() []string {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	return t.ProgressGroup.getDisplayOrder()
}

// GetVisibleTasks exposes getVisibleTasks for testing.
func (t *ProgressGroupForTest) GetVisibleTasks() []string {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	return t.ProgressGroup.getVisibleTasks()
}

// FormatTaskLine exposes formatTaskLine for testing.
func (t *ProgressGroupForTest) FormatTaskLine(name string, state TaskState) string {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	return t.ProgressGroup.formatTaskLine(name, state)
}

// SetTaskStatusForTest sets a task's status in the internal map.
func (t *ProgressGroupForTest) SetTaskStatusForTest(name string, state TaskState) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.taskStatus[name] = state
}

// AddTaskOrderForTest appends a task name to the task order.
func (t *ProgressGroupForTest) AddTaskOrderForTest(name string) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.taskOrder = append(t.ProgressGroup.taskOrder, name)
}

// AddTaskStartOrderForTest appends a task name to the start order.
func (t *ProgressGroupForTest) AddTaskStartOrderForTest(name string) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.taskStartOrder = append(t.ProgressGroup.taskStartOrder, name)
}

// SetMaxVisibleForTest sets maxVisible for testing.
func (t *ProgressGroupForTest) SetMaxVisibleForTest(n int) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.maxVisible = n
}

// SetTermWidthForTest sets termWidth for testing.
func (t *ProgressGroupForTest) SetTermWidthForTest(w int) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.termWidth = w
}

// SetCompletedCountForTest sets completedCount for testing.
func (t *ProgressGroupForTest) SetCompletedCountForTest(n int) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.completedCount = n
}

// SetFailedCountForTest sets failedCount for testing.
func (t *ProgressGroupForTest) SetFailedCountForTest(n int) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.failedCount = n
}

// FormatSummaryForTest exposes formatSummary for testing.
func (t *ProgressGroupForTest) FormatSummaryForTest() string {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	return t.ProgressGroup.formatSummary()
}

// BuildCompactWindowForTest exposes buildCompactWindow for testing.
// Returns the buffer contents and the number of content lines.
func (t *ProgressGroupForTest) BuildCompactWindowForTest() (string, int) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	var buf bytes.Buffer

	lines := t.ProgressGroup.buildCompactWindow(&buf)

	return buf.String(), lines
}

// WriteSummaryLineForTest exposes writeSummaryLine for testing.
func (t *ProgressGroupForTest) WriteSummaryLineForTest() string {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	var buf bytes.Buffer

	t.ProgressGroup.writeSummaryLine(&buf)

	return buf.String()
}

// EmitCompletedForTest exposes emitCompleted for testing.
func (t *ProgressGroupForTest) EmitCompletedForTest() (string, int) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	var buf bytes.Buffer

	count := t.ProgressGroup.emitCompleted(&buf)

	return buf.String(), count
}

// RunTaskForTest exposes runTask for testing.
func (t *ProgressGroupForTest) RunTaskForTest(ctx context.Context, task ProgressTask) error {
	return t.ProgressGroup.runTask(ctx, task)
}

// ExecuteTasksForTest exposes executeTasks for testing.
func (t *ProgressGroupForTest) ExecuteTasksForTest(ctx context.Context, tasks []ProgressTask) error {
	return t.ProgressGroup.executeTasks(ctx, tasks)
}

// SetEmittedForTest marks a task as already emitted.
func (t *ProgressGroupForTest) SetEmittedForTest(name string) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.emitted[name] = true
}

// PrintAllLinesForTest exposes printAllLines for testing.
func (t *ProgressGroupForTest) PrintAllLinesForTest() {
	t.ProgressGroup.printAllLines()
}

// RedrawAllLinesForTest exposes redrawAllLines for testing.
func (t *ProgressGroupForTest) RedrawAllLinesForTest() {
	t.ProgressGroup.redrawAllLines()
}

// GetLinesDrawnForTest returns the number of lines drawn.
func (t *ProgressGroupForTest) GetLinesDrawnForTest() int {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	return t.ProgressGroup.linesDrawn
}

// DrawStreamingZoneForTest exposes drawStreamingZone for testing.
func (t *ProgressGroupForTest) DrawStreamingZoneForTest() {
	t.ProgressGroup.drawStreamingZone()
}

// RedrawStreamingZoneForTest exposes redrawStreamingZone for testing.
func (t *ProgressGroupForTest) RedrawStreamingZoneForTest() {
	t.ProgressGroup.redrawStreamingZone()
}

// FinalizeStreamingOutputForTest exposes finalizeStreamingOutput for testing.
func (t *ProgressGroupForTest) FinalizeStreamingOutputForTest() {
	t.ProgressGroup.finalizeStreamingOutput()
}

// RunSpinnerForTest starts the spinner and returns a stop function.
func (t *ProgressGroupForTest) RunSpinnerForTest() func() {
	go t.ProgressGroup.runSpinner()

	return func() {
		close(t.ProgressGroup.stopSpinner)
		<-t.ProgressGroup.spinnerDone
	}
}

// SetLinesDrawnForTest sets the lines drawn count.
func (t *ProgressGroupForTest) SetLinesDrawnForTest(n int) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.linesDrawn = n
}

// SetAppendOnlyForTest sets appendOnly mode.
func (t *ProgressGroupForTest) SetAppendOnlyForTest(v bool) {
	t.ProgressGroup.mu.Lock()
	defer t.ProgressGroup.mu.Unlock()

	t.ProgressGroup.appendOnly = v
}

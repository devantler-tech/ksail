package notify_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// startsWithEmoji tests
// =============================================================================

func TestStartsWithEmoji(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		want  bool
	}{
		{"empty data", []byte{}, false},
		{"rocket emoji", []byte("🚀 deploy"), true},
		{"package emoji", []byte("📦 install"), true},
		{"plug emoji", []byte("🔌 attach"), true},
		{"file cabinet emoji", []byte("🗄️ registry"), true},
		{"activity symbol ►", []byte("► running"), false},
		{"success symbol ✔", []byte("✔ done"), false},
		{"error symbol ✗", []byte("✗ failed"), false},
		{"warning symbol ⚠", []byte("⚠ caution"), false},
		{"info symbol ℹ", []byte("ℹ info"), false},
		{"generate symbol ✚", []byte("✚ generated"), false},
		{"timer symbol ⏲", []byte("⏲ timing"), false},
		{"ascii text", []byte("hello world"), false},
		{"number", []byte("123"), false},
		{"invalid utf8", []byte{0xFF, 0xFE}, false},
		{"single byte", []byte{0x41}, false},      // 'A'
		{"single emoji byte", []byte("🚀"), true},  // just emoji
		{"gear emoji", []byte("⚙️ config"), true}, // "Other Symbol"
		{"check mark emoji", []byte("✅ validated"), true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := notify.StartsWithEmojiForTest(testCase.input)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// =============================================================================
// appendSlots tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestAppendSlots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result []string
		items  []string
		limit  int
		want   []string
	}{
		{
			name:   "empty result and items",
			result: []string{},
			items:  []string{},
			limit:  5,
			want:   []string{},
		},
		{
			name:   "result already at limit",
			result: []string{"a", "b", "c"},
			items:  []string{"x", "y"},
			limit:  3,
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "result above limit",
			result: []string{"a", "b", "c", "d"},
			items:  []string{"x"},
			limit:  3,
			want:   []string{"a", "b", "c", "d"},
		},
		{
			name:   "fills remaining slots from tail",
			result: []string{"a"},
			items:  []string{"x", "y", "z"},
			limit:  3,
			want:   []string{"a", "y", "z"},
		},
		{
			name:   "appends all items when fewer than remaining slots",
			result: []string{"a"},
			items:  []string{"x"},
			limit:  5,
			want:   []string{"a", "x"},
		},
		{
			name:   "empty items does nothing",
			result: []string{"a"},
			items:  []string{},
			limit:  5,
			want:   []string{"a"},
		},
		{
			name:   "empty result fills from tail",
			result: []string{},
			items:  []string{"a", "b", "c", "d", "e"},
			limit:  3,
			want:   []string{"c", "d", "e"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := notify.AppendSlotsForTest(testCase.result, testCase.items, testCase.limit)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// =============================================================================
// getMessageConfig tests
// =============================================================================

func TestGetMessageConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		msgType    notify.MessageType
		wantSymbol string
	}{
		{"error type", notify.ErrorType, "✗ "},
		{"warning type", notify.WarningType, "⚠ "},
		{"activity type", notify.ActivityType, "► "},
		{"generate type", notify.GenerateType, "✚ "},
		{"success type", notify.SuccessType, "✔ "},
		{"info type", notify.InfoType, "ℹ "},
		{"title type", notify.TitleType, ""},
		{"unknown type", notify.MessageType(99), ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// We can test via WriteMessage and check the output symbol
			var buf bytes.Buffer

			notify.WriteMessage(notify.Message{
				Type:    testCase.msgType,
				Content: "test",
				Writer:  &buf,
			})

			got := buf.String()

			if testCase.msgType == notify.TitleType {
				// Title uses emoji instead of symbol
				assert.Contains(t, got, "test")
			} else if testCase.wantSymbol != "" {
				assert.True(t, strings.HasPrefix(got, testCase.wantSymbol),
					"expected output to start with %q, got %q", testCase.wantSymbol, got)
			}
		})
	}
}

// =============================================================================
// indentMultilineContent tests
// =============================================================================

func TestIndentMultilineContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		indent  string
		want    string
	}{
		{
			name:    "single line with indent",
			content: "hello",
			indent:  "  ",
			want:    "hello",
		},
		{
			name:    "empty indent returns unchanged",
			content: "line1\nline2",
			indent:  "",
			want:    "line1\nline2",
		},
		{
			name:    "multiline with indent",
			content: "first\nsecond\nthird",
			indent:  "  ",
			want:    "first\n  second\n  third",
		},
		{
			name:    "empty lines not indented",
			content: "first\n\nthird",
			indent:  "  ",
			want:    "first\n\n  third",
		},
		{
			name:    "single character content",
			content: "x",
			indent:  "    ",
			want:    "x",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := notify.IndentMultilineContentForTest(testCase.content, testCase.indent)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// =============================================================================
// fitName tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestFitName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		taskName  string
		termWidth int
		wantTrunc bool
	}{
		{
			name:      "short name not truncated",
			taskName:  "nginx",
			termWidth: 80,
			wantTrunc: false,
		},
		{
			name:      "non-TTY mode returns name unchanged",
			taskName:  "a-very-long-name-that-would-overflow-in-tty-mode",
			termWidth: 0,
			wantTrunc: false,
		},
		{
			name:      "long name truncated with ellipsis",
			taskName:  strings.Repeat("a", 100),
			termWidth: 40,
			wantTrunc: true,
		},
		{
			name:      "name at exact limit not truncated",
			taskName:  "short",
			termWidth: 80,
			wantTrunc: false,
		},
		{
			name:      "very narrow terminal uses min name length",
			taskName:  strings.Repeat("x", 20),
			termWidth: 10,
			wantTrunc: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			pgTest.SetTermWidthForTest(testCase.termWidth)

			got := pgTest.FitName(testCase.taskName, notify.TaskRunningForTest)

			if testCase.wantTrunc {
				assert.Less(t, len(got), len(testCase.taskName),
					"expected truncated name")
				assert.Contains(t, got, "…",
					"expected middle truncation with ellipsis")
			} else {
				assert.Equal(t, testCase.taskName, got)
			}
		})
	}
}

func TestFitName_DifferentStates(t *testing.T) {
	t.Parallel()

	states := []struct {
		name  string
		state notify.TaskState
	}{
		{"pending", notify.TaskPendingForTest},
		{"running", notify.TaskRunningForTest},
		{"complete", notify.TaskCompleteForTest},
		{"failed", notify.TaskFailedForTest},
	}

	longName := strings.Repeat("component-", 10) // 100 chars

	for _, stateCase := range states {
		t.Run(stateCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			pgTest.SetTermWidthForTest(50) // narrow terminal

			got := pgTest.FitName(longName, stateCase.state)

			assert.Less(t, len(got), len(longName),
				"expected name to be truncated for state %s", stateCase.name)
			assert.Contains(t, got, "…")
		})
	}
}

func TestFitName_WithTimer(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tmr := &fixedTimer{}

	pg := notify.NewProgressGroup("Test", "►", &buf, notify.WithTimer(tmr))
	pgTest := notify.NewProgressGroupForTest(pg)
	pgTest.SetTermWidthForTest(50)

	longName := strings.Repeat("a", 80)
	got := pgTest.FitName(longName, notify.TaskCompleteForTest)

	// With timer, the suffix is longer so name should be truncated more aggressively
	assert.Less(t, len(got), len(longName))
	assert.Contains(t, got, "…")
}

// =============================================================================
// compactWindowHeight tests
// =============================================================================

func TestCompactWindowHeight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		maxVisible int
		wantHeight int
	}{
		{"maxVisible 1", 1, 3},
		{"maxVisible 5", 5, 7},
		{"maxVisible 10", 10, 12},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			pgTest.SetMaxVisibleForTest(testCase.maxVisible)

			got := pgTest.CompactWindowHeight()
			assert.Equal(t, testCase.wantHeight, got)
		})
	}
}

// =============================================================================
// countPending tests
// =============================================================================

func TestCountPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		states map[string]notify.TaskState
		want   int
	}{
		{
			name:   "no tasks",
			states: map[string]notify.TaskState{},
			want:   0,
		},
		{
			name: "all pending",
			states: map[string]notify.TaskState{
				"a": notify.TaskPendingForTest,
				"b": notify.TaskPendingForTest,
				"c": notify.TaskPendingForTest,
			},
			want: 3,
		},
		{
			name: "mixed states",
			states: map[string]notify.TaskState{
				"a": notify.TaskPendingForTest,
				"b": notify.TaskRunningForTest,
				"c": notify.TaskCompleteForTest,
				"d": notify.TaskPendingForTest,
				"e": notify.TaskFailedForTest,
			},
			want: 2,
		},
		{
			name: "none pending",
			states: map[string]notify.TaskState{
				"a": notify.TaskRunningForTest,
				"b": notify.TaskCompleteForTest,
			},
			want: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)

			for name, state := range testCase.states {
				pgTest.AddTaskOrderForTest(name)
				pgTest.SetTaskStatusForTest(name, state)
			}

			got := pgTest.CountPending()
			assert.Equal(t, testCase.want, got)
		})
	}
}

// =============================================================================
// getDisplayOrder tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestGetDisplayOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		taskOrder      []string
		taskStartOrder []string
		taskStates     map[string]notify.TaskState
		want           []string
	}{
		{
			name:           "no tasks",
			taskOrder:      []string{},
			taskStartOrder: []string{},
			taskStates:     map[string]notify.TaskState{},
			want:           []string{},
		},
		{
			name:           "all pending - original order",
			taskOrder:      []string{"a", "b", "c"},
			taskStartOrder: []string{},
			taskStates: map[string]notify.TaskState{
				"a": notify.TaskPendingForTest,
				"b": notify.TaskPendingForTest,
				"c": notify.TaskPendingForTest,
			},
			want: []string{"a", "b", "c"},
		},
		{
			name:           "started tasks first then pending",
			taskOrder:      []string{"a", "b", "c", "d"},
			taskStartOrder: []string{"c", "a"},
			taskStates: map[string]notify.TaskState{
				"a": notify.TaskRunningForTest,
				"b": notify.TaskPendingForTest,
				"c": notify.TaskCompleteForTest,
				"d": notify.TaskPendingForTest,
			},
			want: []string{"c", "a", "b", "d"},
		},
		{
			name:           "all started",
			taskOrder:      []string{"a", "b", "c"},
			taskStartOrder: []string{"b", "c", "a"},
			taskStates: map[string]notify.TaskState{
				"a": notify.TaskCompleteForTest,
				"b": notify.TaskRunningForTest,
				"c": notify.TaskFailedForTest,
			},
			want: []string{"b", "c", "a"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)

			for _, name := range testCase.taskOrder {
				pgTest.AddTaskOrderForTest(name)
			}

			for _, name := range testCase.taskStartOrder {
				pgTest.AddTaskStartOrderForTest(name)
			}

			for name, state := range testCase.taskStates {
				pgTest.SetTaskStatusForTest(name, state)
			}

			got := pgTest.GetDisplayOrder()
			assert.Equal(t, testCase.want, got)
		})
	}
}

// =============================================================================
// getVisibleTasks tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestGetVisibleTasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		maxVisible     int
		taskStartOrder []string
		taskStates     map[string]notify.TaskState
		wantLen        int
	}{
		{
			name:           "no tasks",
			maxVisible:     5,
			taskStartOrder: []string{},
			taskStates:     map[string]notify.TaskState{},
			wantLen:        0,
		},
		{
			name:       "running tasks included first",
			maxVisible: 3,
			taskStartOrder: []string{
				"a", "b", "c",
			},
			taskStates: map[string]notify.TaskState{
				"a": notify.TaskRunningForTest,
				"b": notify.TaskCompleteForTest,
				"c": notify.TaskRunningForTest,
			},
			wantLen: 3,
		},
		{
			name:       "caps to maxVisible",
			maxVisible: 2,
			taskStartOrder: []string{
				"a", "b", "c", "d", "e",
			},
			taskStates: map[string]notify.TaskState{
				"a": notify.TaskRunningForTest,
				"b": notify.TaskRunningForTest,
				"c": notify.TaskRunningForTest,
				"d": notify.TaskCompleteForTest,
				"e": notify.TaskFailedForTest,
			},
			wantLen: 2,
		},
		{
			name:       "failed tasks shown after running",
			maxVisible: 3,
			taskStartOrder: []string{
				"a", "b", "c",
			},
			taskStates: map[string]notify.TaskState{
				"a": notify.TaskCompleteForTest,
				"b": notify.TaskFailedForTest,
				"c": notify.TaskRunningForTest,
			},
			wantLen: 3,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			pgTest.SetMaxVisibleForTest(testCase.maxVisible)

			for _, name := range testCase.taskStartOrder {
				pgTest.AddTaskStartOrderForTest(name)
				pgTest.AddTaskOrderForTest(name)
			}

			for name, state := range testCase.taskStates {
				pgTest.SetTaskStatusForTest(name, state)
			}

			got := pgTest.GetVisibleTasks()
			assert.Len(t, got, testCase.wantLen)
			assert.LessOrEqual(t, len(got), testCase.maxVisible)
		})
	}
}

// =============================================================================
// formatTaskLine tests
// =============================================================================

func TestFormatTaskLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		taskName    string
		state       notify.TaskState
		wantContain string
	}{
		{
			name:        "pending task",
			taskName:    "my-task",
			state:       notify.TaskPendingForTest,
			wantContain: "my-task",
		},
		{
			name:        "running task",
			taskName:    "my-task",
			state:       notify.TaskRunningForTest,
			wantContain: "my-task",
		},
		{
			name:        "complete task",
			taskName:    "my-task",
			state:       notify.TaskCompleteForTest,
			wantContain: "my-task",
		},
		{
			name:        "failed task",
			taskName:    "my-task",
			state:       notify.TaskFailedForTest,
			wantContain: "my-task",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			pgTest.AddTaskOrderForTest(testCase.taskName)
			pgTest.SetTaskStatusForTest(testCase.taskName, testCase.state)

			got := pgTest.FormatTaskLine(testCase.taskName, testCase.state)

			require.NotEmpty(t, got)
			assert.Contains(t, got, testCase.wantContain)
		})
	}
}

func TestFormatTaskLine_UnknownState(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	pg := notify.NewProgressGroup("Test", "►", &buf)
	pgTest := notify.NewProgressGroupForTest(pg)

	got := pgTest.FormatTaskLine("my-task", notify.TaskState(99))

	assert.Contains(t, got, "my-task")
	assert.Contains(t, got, "unknown")
}

func TestFormatTaskLine_PendingContainsPendingLabel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	pg := notify.NewProgressGroup("Test", "►", &buf,
		notify.WithLabels(notify.InstallingLabels()),
	)
	pgTest := notify.NewProgressGroupForTest(pg)

	got := pgTest.FormatTaskLine("component", notify.TaskPendingForTest)

	assert.Contains(t, got, "pending")
	assert.Contains(t, got, "○")
}

func TestFormatTaskLine_RunningContainsRunningLabel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	pg := notify.NewProgressGroup("Test", "►", &buf,
		notify.WithLabels(notify.InstallingLabels()),
	)
	pgTest := notify.NewProgressGroupForTest(pg)

	got := pgTest.FormatTaskLine("component", notify.TaskRunningForTest)

	assert.Contains(t, got, "installing")
}

func TestFormatTaskLine_CompleteContainsCompletedLabel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	pg := notify.NewProgressGroup("Test", "►", &buf,
		notify.WithLabels(notify.InstallingLabels()),
	)
	pgTest := notify.NewProgressGroupForTest(pg)

	got := pgTest.FormatTaskLine("component", notify.TaskCompleteForTest)

	assert.Contains(t, got, "installed")
	assert.Contains(t, got, "✔")
}

func TestFormatTaskLine_FailedContainsFailed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	pg := notify.NewProgressGroup("Test", "►", &buf)
	pgTest := notify.NewProgressGroupForTest(pg)

	got := pgTest.FormatTaskLine("component", notify.TaskFailedForTest)

	assert.Contains(t, got, "failed")
	assert.Contains(t, got, "✗")
}

// =============================================================================
// formatSummary tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestFormatSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		completed  int
		failed     int
		countLabel string
		wantEmpty  bool
		wantParts  []string
	}{
		{
			name:      "no completed or failed",
			completed: 0,
			failed:    0,
			wantEmpty: true,
		},
		{
			name:      "completed only",
			completed: 5,
			failed:    0,
			wantParts: []string{"5", "completed"},
		},
		{
			name:      "failed only",
			completed: 0,
			failed:    3,
			wantParts: []string{"3", "failed"},
		},
		{
			name:      "completed and failed",
			completed: 10,
			failed:    2,
			wantParts: []string{"10", "completed", "2", "failed"},
		},
		{
			name:       "with count label",
			completed:  62,
			failed:     0,
			countLabel: "kustomizations",
			wantParts:  []string{"62", "kustomizations", "completed"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			opts := []notify.ProgressOption{}
			if testCase.countLabel != "" {
				opts = append(opts, notify.WithCountLabel(testCase.countLabel))
			}

			pg := notify.NewProgressGroup("Test", "►", &buf, opts...)
			pgTest := notify.NewProgressGroupForTest(pg)
			pgTest.SetCompletedCountForTest(testCase.completed)
			pgTest.SetFailedCountForTest(testCase.failed)

			got := pgTest.FormatSummaryForTest()

			if testCase.wantEmpty {
				assert.Empty(t, got)
			} else {
				for _, part := range testCase.wantParts {
					assert.Contains(t, got, part)
				}
			}
		})
	}
}

// =============================================================================
// StageSeparatingWriter error path tests
// =============================================================================

func TestStageSeparatingWriter_WriteErrorOnSeparator(t *testing.T) {
	t.Parallel()

	// Use a writer that succeeds on the first call, then fails on subsequent calls.
	// This allows hasWritten to be set, then the separator write fails.
	w := &countingFailWriter{failAfter: 1}
	writer := notify.NewStageSeparatingWriter(w)

	// First write succeeds → hasWritten=true
	_, err := writer.Write([]byte("first line"))
	require.NoError(t, err)

	// Second write with emoji: tries to write separator newline → fails
	_, err = writer.Write([]byte("🚀 title"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write stage separator")
}

func TestStageSeparatingWriter_WriteErrorOnData(t *testing.T) {
	t.Parallel()

	// Use a writer that succeeds on first call, fails on second
	w := &countingFailWriter{failAfter: 1}
	writer := notify.NewStageSeparatingWriter(w)

	// First write should succeed
	n, err := writer.Write([]byte("hello"))

	require.NoError(t, err)
	assert.Equal(t, 5, n)

	// Second write with non-emoji (no separator attempted, direct data write fails)
	_, err = writer.Write([]byte("world"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write data")
}

// countingFailWriter tracks write calls and fails after a certain count.
type countingFailWriter struct {
	calls     int
	failAfter int
}

func (w *countingFailWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.calls > w.failAfter {
		return 0, errNotifyWriterFailed
	}

	return len(data), nil
}

// =============================================================================
// WriteMessage with GenerateType
// =============================================================================

func TestWriteMessage_GenerateType(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	notify.WriteMessage(notify.Message{
		Type:    notify.GenerateType,
		Content: "generated file",
		Writer:  &out,
	})

	got := out.String()
	want := "✚ generated file\n"

	assert.Equal(t, want, got)
}

// =============================================================================
// WithClock nil test
// =============================================================================

func TestWithClock_NilIgnored(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// WithClock(nil) should be a no-op
	pg := notify.NewProgressGroup("Test", "►", &buf, notify.WithClock(nil))

	// Should not panic - just verify it creates successfully
	require.NotNil(t, pg)
}

// =============================================================================
// buildCompactWindow tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestBuildCompactWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(pgTest *notify.ProgressGroupForTest)
		wantLines int
		wantParts []string
	}{
		{
			name: "no completed or failed, only pending",
			setup: func(pgTest *notify.ProgressGroupForTest) {
				pgTest.SetMaxVisibleForTest(5)
				pgTest.AddTaskOrderForTest("a")
				pgTest.AddTaskOrderForTest("b")
				pgTest.SetTaskStatusForTest("a", notify.TaskPendingForTest)
				pgTest.SetTaskStatusForTest("b", notify.TaskPendingForTest)
			},
			wantLines: 1, // only remaining count line
			wantParts: []string{"2 remaining"},
		},
		{
			name: "completed tasks show summary",
			setup: func(pgTest *notify.ProgressGroupForTest) {
				pgTest.SetMaxVisibleForTest(3)
				pgTest.SetCompletedCountForTest(5)
				pgTest.AddTaskOrderForTest("a")
				pgTest.AddTaskStartOrderForTest("a")
				pgTest.SetTaskStatusForTest("a", notify.TaskRunningForTest)
			},
			wantLines: 2, // summary + 1 running task (no pending remaining line)
			wantParts: []string{"5"},
		},
		{
			name: "mixed running and pending tasks",
			setup: func(pgTest *notify.ProgressGroupForTest) {
				pgTest.SetMaxVisibleForTest(3)
				pgTest.SetCompletedCountForTest(2)
				pgTest.AddTaskOrderForTest("a")
				pgTest.AddTaskOrderForTest("b")
				pgTest.AddTaskOrderForTest("c")
				pgTest.AddTaskStartOrderForTest("a")
				pgTest.SetTaskStatusForTest("a", notify.TaskRunningForTest)
				pgTest.SetTaskStatusForTest("b", notify.TaskPendingForTest)
				pgTest.SetTaskStatusForTest("c", notify.TaskPendingForTest)
			},
			wantLines: 3, // summary + 1 visible task + remaining count
			wantParts: []string{"2", "remaining"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			testCase.setup(pgTest)

			output, lineCount := pgTest.BuildCompactWindowForTest()

			assert.Equal(t, testCase.wantLines, lineCount)

			for _, part := range testCase.wantParts {
				assert.Contains(t, output, part)
			}
		})
	}
}

// =============================================================================
// writeSummaryLine tests
// =============================================================================

func TestWriteSummaryLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		completed int
		failed    int
		wantEmpty bool
	}{
		{"no completed or failed", 0, 0, true},
		{"has completed", 3, 0, false},
		{"has failed", 0, 2, false},
		{"has both", 5, 1, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			pgTest.SetCompletedCountForTest(testCase.completed)
			pgTest.SetFailedCountForTest(testCase.failed)

			got := pgTest.WriteSummaryLineForTest()

			if testCase.wantEmpty {
				assert.Empty(t, got)
			} else {
				assert.NotEmpty(t, got)
			}
		})
	}
}

// =============================================================================
// emitCompleted tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestEmitCompleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(pgTest *notify.ProgressGroupForTest)
		wantCount int
		wantParts []string
	}{
		{
			name: "no completed tasks",
			setup: func(pgTest *notify.ProgressGroupForTest) {
				pgTest.AddTaskOrderForTest("a")
				pgTest.AddTaskStartOrderForTest("a")
				pgTest.SetTaskStatusForTest("a", notify.TaskRunningForTest)
			},
			wantCount: 0,
		},
		{
			name: "emits newly completed tasks",
			setup: func(pgTest *notify.ProgressGroupForTest) {
				pgTest.AddTaskOrderForTest("a")
				pgTest.AddTaskOrderForTest("b")
				pgTest.AddTaskStartOrderForTest("a")
				pgTest.AddTaskStartOrderForTest("b")
				pgTest.SetTaskStatusForTest("a", notify.TaskCompleteForTest)
				pgTest.SetTaskStatusForTest("b", notify.TaskFailedForTest)
			},
			wantCount: 2,
			wantParts: []string{"a", "b"},
		},
		{
			name: "skips already-emitted tasks",
			setup: func(pgTest *notify.ProgressGroupForTest) {
				pgTest.AddTaskOrderForTest("a")
				pgTest.AddTaskStartOrderForTest("a")
				pgTest.SetTaskStatusForTest("a", notify.TaskCompleteForTest)
				pgTest.SetEmittedForTest("a")
			},
			wantCount: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			pg := notify.NewProgressGroup("Test", "►", &buf)
			pgTest := notify.NewProgressGroupForTest(pg)
			testCase.setup(pgTest)

			output, count := pgTest.EmitCompletedForTest()

			assert.Equal(t, testCase.wantCount, count)

			for _, part := range testCase.wantParts {
				assert.Contains(t, output, part)
			}
		})
	}
}

// =============================================================================
// runTask tests
// =============================================================================

func TestRunTask(t *testing.T) {
	t.Parallel()

	t.Run("successful task transitions to complete", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)
		pgTest.AddTaskOrderForTest("my-task")
		pgTest.SetTaskStatusForTest("my-task", notify.TaskPendingForTest)

		task := notify.ProgressTask{
			Name: "my-task",
			Fn:   func(_ context.Context) error { return nil },
		}

		err := pgTest.RunTaskForTest(context.Background(), task)

		require.NoError(t, err)
	})

	t.Run("failing task transitions to failed", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)
		pgTest.AddTaskOrderForTest("bad-task")
		pgTest.SetTaskStatusForTest("bad-task", notify.TaskPendingForTest)

		task := notify.ProgressTask{
			Name: "bad-task",
			Fn:   func(_ context.Context) error { return errNotifyWriterFailed },
		}

		err := pgTest.RunTaskForTest(context.Background(), task)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad-task")
	})
}

// =============================================================================
// executeTasks tests
// =============================================================================

func TestExecuteTasks(t *testing.T) {
	t.Parallel()

	t.Run("fail-fast mode cancels on first error", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		for _, name := range []string{"a", "b"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		tasks := []notify.ProgressTask{
			{Name: "a", Fn: func(_ context.Context) error { return errNotifyWriterFailed }},
			{Name: "b", Fn: func(_ context.Context) error { return nil }},
		}

		err := pgTest.ExecuteTasksForTest(context.Background(), tasks)

		require.Error(t, err)
	})

	t.Run("continue-on-error mode collects all errors", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf, notify.WithContinueOnError())
		pgTest := notify.NewProgressGroupForTest(pg)

		for _, name := range []string{"a", "b", "c"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		tasks := []notify.ProgressTask{
			{Name: "a", Fn: func(_ context.Context) error { return errNotifyWriterFailed }},
			{Name: "b", Fn: func(_ context.Context) error { return nil }},
			{Name: "c", Fn: func(_ context.Context) error { return errNotifyWriterFailed }},
		}

		err := pgTest.ExecuteTasksForTest(context.Background(), tasks)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "a")
		assert.Contains(t, err.Error(), "c")
	})
}

// =============================================================================
// printAllLines tests
// =============================================================================

func TestPrintAllLines(t *testing.T) {
	t.Parallel()

	t.Run("standard mode prints all tasks", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		for _, name := range []string{"task-a", "task-b", "task-c"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		pgTest.PrintAllLinesForTest()

		output := buf.String()
		assert.Contains(t, output, "task-a")
		assert.Contains(t, output, "task-b")
		assert.Contains(t, output, "task-c")
		assert.Equal(t, 3, pgTest.GetLinesDrawnForTest())
	})

	t.Run("compact mode uses compact output", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)
		pgTest.SetMaxVisibleForTest(2)

		for _, name := range []string{"task-a", "task-b", "task-c"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		pgTest.PrintAllLinesForTest()

		output := buf.String()
		// Should contain the remaining count
		assert.Contains(t, output, "remaining")
	})
}

// =============================================================================
// redrawAllLines tests
// =============================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestRedrawAllLines(t *testing.T) {
	t.Parallel()

	t.Run("no-op when linesDrawn is zero", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		// No lines drawn - redraw should be a no-op
		pgTest.RedrawAllLinesForTest()

		assert.Empty(t, buf.String())
	})

	t.Run("redraws standard mode tasks", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		// Set up tasks and simulate initial draw
		for _, name := range []string{"task-a", "task-b"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		// First draw
		pgTest.PrintAllLinesForTest()
		buf.Reset()

		// Change a task state
		pgTest.SetTaskStatusForTest("task-a", notify.TaskRunningForTest)
		pgTest.AddTaskStartOrderForTest("task-a")

		// Redraw
		pgTest.RedrawAllLinesForTest()

		output := buf.String()
		// Should contain ANSI escape for cursor movement
		assert.Contains(t, output, "\033[")
		assert.Contains(t, output, "task-a")
	})

	t.Run("redraws compact mode tasks", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)
		pgTest.SetMaxVisibleForTest(2)

		for _, name := range []string{"a", "b", "c"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		pgTest.PrintAllLinesForTest()
		buf.Reset()

		pgTest.SetCompletedCountForTest(1)
		pgTest.SetTaskStatusForTest("a", notify.TaskCompleteForTest)
		pgTest.AddTaskStartOrderForTest("a")

		pgTest.RedrawAllLinesForTest()

		output := buf.String()
		assert.Contains(t, output, "\033[")
	})
}

// =============================================================================
// drawStreamingZone tests
// =============================================================================

func TestDrawStreamingZone(t *testing.T) {
	t.Parallel()

	t.Run("draws initial pending tasks", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		for _, name := range []string{"a", "b", "c", "d", "e"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		pgTest.DrawStreamingZoneForTest()

		output := buf.String()
		// Default streamingPendingPreview is 3, should show only 3 tasks
		assert.Contains(t, output, "a")
		assert.Contains(t, output, "b")
		assert.Contains(t, output, "c")
		assert.Equal(t, 3, pgTest.GetLinesDrawnForTest())
	})

	t.Run("with concurrency shows more tasks", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf, notify.WithConcurrency(2))
		pgTest := notify.NewProgressGroupForTest(pg)

		for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		pgTest.DrawStreamingZoneForTest()

		// concurrency(2) + streamingPendingPreview(3) = 5
		assert.Equal(t, 5, pgTest.GetLinesDrawnForTest())
	})
}

// =============================================================================
// redrawStreamingZone tests
// =============================================================================

func TestRedrawStreamingZone(t *testing.T) {
	t.Parallel()

	t.Run("no-op when all emitted and no lines drawn", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		pgTest.AddTaskOrderForTest("a")
		pgTest.SetTaskStatusForTest("a", notify.TaskCompleteForTest)
		pgTest.AddTaskStartOrderForTest("a")
		pgTest.SetEmittedForTest("a")
		pgTest.SetLinesDrawnForTest(0)

		pgTest.RedrawStreamingZoneForTest()

		assert.Empty(t, buf.String())
	})

	t.Run("redraws with running and pending tasks", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		for _, name := range []string{"a", "b", "c", "d"} {
			pgTest.AddTaskOrderForTest(name)
			pgTest.SetTaskStatusForTest(name, notify.TaskPendingForTest)
		}

		// Simulate initial draw
		pgTest.DrawStreamingZoneForTest()
		buf.Reset()

		// Start task "a"
		pgTest.SetTaskStatusForTest("a", notify.TaskRunningForTest)
		pgTest.AddTaskStartOrderForTest("a")

		pgTest.RedrawStreamingZoneForTest()

		output := buf.String()
		assert.Contains(t, output, "\033[") // ANSI cursor movement
	})
}

// =============================================================================
// finalizeStreamingOutput tests
// =============================================================================

func TestFinalizeStreamingOutput(t *testing.T) {
	t.Parallel()

	t.Run("emits remaining completed tasks and clears zone", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		pgTest.AddTaskOrderForTest("a")
		pgTest.AddTaskStartOrderForTest("a")
		pgTest.SetTaskStatusForTest("a", notify.TaskCompleteForTest)

		// Simulate some drawn lines
		pgTest.SetLinesDrawnForTest(2)

		pgTest.FinalizeStreamingOutputForTest()

		output := buf.String()
		assert.Contains(t, output, "a")
		assert.Equal(t, 0, pgTest.GetLinesDrawnForTest())
	})

	t.Run("handles no lines drawn", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		pgTest.AddTaskOrderForTest("a")
		pgTest.AddTaskStartOrderForTest("a")
		pgTest.SetTaskStatusForTest("a", notify.TaskCompleteForTest)
		pgTest.SetLinesDrawnForTest(0)

		pgTest.FinalizeStreamingOutputForTest()

		assert.Equal(t, 0, pgTest.GetLinesDrawnForTest())
	})
}

// =============================================================================
// runSpinner tests
// =============================================================================

func TestRunSpinner(t *testing.T) {
	t.Parallel()

	t.Run("spinner starts and stops cleanly", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf)
		pgTest := notify.NewProgressGroupForTest(pg)

		// Set up some tasks and lines so spinner has something to redraw
		pgTest.AddTaskOrderForTest("a")
		pgTest.SetTaskStatusForTest("a", notify.TaskRunningForTest)
		pgTest.PrintAllLinesForTest()

		stop := pgTest.RunSpinnerForTest()

		// Let the spinner tick a few times
		// spinnerTickInterval is 100ms, so 250ms should give 2+ ticks
		<-time.After(250 * time.Millisecond)

		stop()

		// If we get here without hanging, spinner stopped cleanly
	})

	t.Run("append-only spinner uses streaming zone", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		pg := notify.NewProgressGroup("Test", "►", &buf, notify.WithAppendOnly())
		pgTest := notify.NewProgressGroupForTest(pg)

		pgTest.AddTaskOrderForTest("a")
		pgTest.SetTaskStatusForTest("a", notify.TaskRunningForTest)
		pgTest.AddTaskStartOrderForTest("a")
		pgTest.DrawStreamingZoneForTest()
		pgTest.SetAppendOnlyForTest(true)

		stop := pgTest.RunSpinnerForTest()

		<-time.After(250 * time.Millisecond)

		stop()

		// If we get here without hanging, spinner stopped cleanly
	})
}

package chat_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
)

// TestAddToPromptHistory_UniqueEntries tests that only unique prompts are added.
func TestAddToPromptHistory_UniqueEntries(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	chat.ExportAddToPromptHistory(model, "first")
	chat.ExportAddToPromptHistory(model, "second")
	chat.ExportAddToPromptHistory(model, "second") // duplicate

	history := chat.ExportGetHistory(model)
	if len(history) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(history))
	}
}

// TestAddToPromptHistory_EmptyIgnored tests that empty prompts are ignored.
func TestAddToPromptHistory_EmptyIgnored(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	chat.ExportAddToPromptHistory(model, "")

	history := chat.ExportGetHistory(model)
	if len(history) != 0 {
		t.Errorf("expected 0 history entries, got %d", len(history))
	}
}

// TestHasRunningTools tests running tool detection.
func TestHasRunningTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tools    map[string]*chat.ToolExecutionForTest
		order    []string
		expected bool
	}{
		{
			name:     "no tools",
			tools:    map[string]*chat.ToolExecutionForTest{},
			order:    nil,
			expected: false,
		},
		{
			name: "running tool",
			tools: map[string]*chat.ToolExecutionForTest{
				"t1": chat.ExportNewToolExecution("bash", chat.ToolStatusRunning, false),
			},
			order:    []string{"t1"},
			expected: true,
		},
		{
			name: "completed tool only",
			tools: map[string]*chat.ToolExecutionForTest{
				"t1": chat.ExportNewToolExecution("bash", chat.ToolStatusComplete, false),
			},
			order:    []string{"t1"},
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetTools(model, testCase.tools, testCase.order)

			if got := chat.ExportHasRunningTools(model); got != testCase.expected {
				t.Errorf("hasRunningTools() = %v, want %v", got, testCase.expected)
			}
		})
	}
}

// TestPeekAndDropPendingPrompts tests peeking and dropping pending prompts.
func TestPeekAndDropPendingPrompts(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// No pending prompts initially
	if chat.ExportPeekNextPendingPrompt(model) {
		t.Error("expected no pending prompts initially")
	}

	// Queue a prompt
	updatedModel := typeText(model, "task one")

	updatedModel, _ = updatedModel.Update(ctrlQKey())

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	// Should have a pending prompt
	if !chat.ExportPeekNextPendingPrompt(chatModel) {
		t.Error("expected a pending prompt after queuing")
	}

	// Drop it
	chat.ExportDropNextPendingPrompt(chatModel)

	if chat.ExportPeekNextPendingPrompt(chatModel) {
		t.Error("expected no pending prompts after dropping")
	}
}

// TestTruncateString tests string truncation with ellipsis.
func TestTruncateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{name: "short string", input: "hello", maxLen: 10, expected: "hello"},
		{name: "exact length", input: "hello", maxLen: 5, expected: "hello"},
		{name: "needs truncation", input: "hello world", maxLen: 8, expected: "hello..."},
		{
			name: "unicode string", input: "\u00e9\u00e0\u00fc\u00f1\u00f8",
			maxLen: 4, expected: "\u00e9...",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportTruncateString(testCase.input, testCase.maxLen)
			if result != testCase.expected {
				t.Errorf(
					"truncateString(%q, %d) = %q, want %q",
					testCase.input,
					testCase.maxLen,
					result,
					testCase.expected,
				)
			}
		})
	}
}

// TestIsValidSessionIndex tests session picker index validation.
func TestIsValidSessionIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		index    int
		sessions int
		expected bool
	}{
		{name: "new chat option", index: 0, sessions: 3, expected: false},
		{name: "first session", index: 1, sessions: 3, expected: true},
		{name: "last session", index: 3, sessions: 3, expected: true},
		{name: "out of bounds", index: 4, sessions: 3, expected: false},
		{name: "negative index", index: -1, sessions: 3, expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			sessions := make([]chat.SessionMetadata, testCase.sessions)
			chat.ExportSetFilteredSessions(model, sessions)
			chat.ExportSetSessionPickerIndex(model, testCase.index)

			if got := chat.ExportIsValidSessionIndex(model); got != testCase.expected {
				t.Errorf("isValidSessionIndex() = %v, want %v", got, testCase.expected)
			}
		})
	}
}

// TestIsInvalidSessionIndex tests the inverse of valid session index.
func TestIsInvalidSessionIndex(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	sessions := make([]chat.SessionMetadata, 2)
	chat.ExportSetFilteredSessions(model, sessions)

	chat.ExportSetSessionPickerIndex(model, 0) // "New Chat"

	if !chat.ExportIsInvalidSessionIndex(model) {
		t.Error("expected index 0 to be invalid for session operations")
	}

	chat.ExportSetSessionPickerIndex(model, 1)

	if chat.ExportIsInvalidSessionIndex(model) {
		t.Error("expected index 1 to be valid for session operations")
	}
}

// TestClampSessionIndex tests clamping of session picker index.
func TestClampSessionIndex(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	sessions := make([]chat.SessionMetadata, 2)
	chat.ExportSetFilteredSessions(model, sessions)

	// Set index beyond range
	chat.ExportSetSessionPickerIndex(model, 5)
	chat.ExportClampSessionIndex(model)

	index := chat.ExportGetSessionPickerIndex(model)
	if index != 2 {
		t.Errorf("expected clamped index 2, got %d", index)
	}

	// Within range should not change
	chat.ExportSetSessionPickerIndex(model, 1)
	chat.ExportClampSessionIndex(model)

	index = chat.ExportGetSessionPickerIndex(model)
	if index != 1 {
		t.Errorf("expected unchanged index 1, got %d", index)
	}
}

// TestFindCurrentSessionIndex tests session index lookup.
func TestFindCurrentSessionIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		sessions  []chat.SessionMetadata
		expected  int
	}{
		{name: "empty session ID", sessionID: "", expected: 0},
		{
			name:      "session found",
			sessionID: "abc-123",
			sessions: []chat.SessionMetadata{
				{ID: "def-456"},
				{ID: "abc-123"},
			},
			expected: 2, // offset by 1 for "New Chat"
		},
		{
			name:      "session not found",
			sessionID: "missing",
			sessions: []chat.SessionMetadata{
				{ID: "abc-123"},
			},
			expected: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetCurrentSessionID(model, testCase.sessionID)
			chat.ExportSetAvailableSessions(model, testCase.sessions)

			if got := chat.ExportFindCurrentSessionIndex(model); got != testCase.expected {
				t.Errorf("findCurrentSessionIndex() = %d, want %d", got, testCase.expected)
			}
		})
	}
}

// TestViewport_WithUserMessages tests that user messages render in the viewport.
func TestViewport_WithUserMessages(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewUserMessage("Hello, how are you?"),
		chat.ExportNewAssistantMessageWithRole("I'm doing well, thanks!"),
	})

	// Resize to reasonable dimensions to ensure rendering works
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "Hello") {
		t.Error("expected user message in viewport")
	}
}

// TestStatusBar_StreamingShowsSpinner tests that spinner is shown during streaming.
func TestStatusBar_StreamingShowsSpinner(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	output := chat.ExportBuildStatusText(model)

	// Spinner should render something (spinner changes over time, but should be non-empty)
	if output == "" {
		t.Error("expected non-empty status text during streaming")
	}

	if !strings.Contains(output, "interactive") {
		t.Error("expected mode label in status text during streaming")
	}
}

// TestStatusBar_JustCompletedShowsReady tests that "Ready" is shown after completion.
func TestStatusBar_JustCompletedShowsReady(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetJustCompleted(model, true)

	output := chat.ExportBuildStatusText(model)

	if !strings.Contains(output, "Ready") {
		t.Error("expected 'Ready' in status text after completion")
	}
}

// TestStatusBar_CopyFeedbackShowsCopied tests that "Copied" is shown after copy.
func TestStatusBar_CopyFeedbackShowsCopied(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowCopyFeedback(model, true)

	output := chat.ExportBuildStatusText(model)

	if !strings.Contains(output, "Copied") {
		t.Error("expected 'Copied' in status text after copy")
	}
}

// TestStatusBar_ModelUnavailableFeedback tests unavailable model feedback.
func TestStatusBar_ModelUnavailableFeedback(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelUnavailableFeedback(model, true)
	chat.ExportSetModelUnavailableReason(model, "rate limited")

	output := chat.ExportBuildStatusText(model)

	if !strings.Contains(output, "Models unavailable") {
		t.Error("expected 'Models unavailable' in status text")
	}

	if !strings.Contains(output, "rate limited") {
		t.Error("expected unavailability reason in status text")
	}
}

// TestStatusBar_ModelUnavailableFeedback_NoReason tests unavailable model without reason.
func TestStatusBar_ModelUnavailableFeedback_NoReason(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelUnavailableFeedback(model, true)
	chat.ExportSetModelUnavailableReason(model, "")

	output := chat.ExportBuildStatusText(model)

	if !strings.Contains(output, "Models unavailable") {
		t.Error("expected 'Models unavailable' in status text even without reason")
	}
}

// TestSessionMetadata_GetDisplayName tests display name hierarchy.
func TestSessionMetadata_GetDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata chat.SessionMetadata
		expected string
	}{
		{
			name:     "with local name",
			metadata: chat.SessionMetadata{Name: "My Chat"},
			expected: "My Chat",
		},
		{
			name:     "empty name falls back to unnamed",
			metadata: chat.SessionMetadata{Name: ""},
			expected: "Unnamed",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := testCase.metadata.GetDisplayName()
			if got != testCase.expected {
				t.Errorf("GetDisplayName() = %q, want %q", got, testCase.expected)
			}
		})
	}
}

// TestSessionPickerHelpParts tests that session picker help parts are rendered.
func TestSessionPickerHelpParts(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowSessionPicker(model, true)

	// Resize to ensure help text renders
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 200, Height: 50})

	output := updatedModel.View()

	// Session picker should have some help text visible
	if output == "" {
		t.Error("expected non-empty view with session picker open")
	}
}

// TestFormatRelativeTime_AllPeriods tests all relative time formatting periods.
func TestFormatRelativeTime_AllPeriods(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{name: "just now", time: now.Add(-30 * time.Second), expected: "just now"},
		{name: "minutes ago", time: now.Add(-5 * time.Minute), expected: "5 mins ago"},
		{name: "hours ago", time: now.Add(-3 * time.Hour), expected: "3 hours ago"},
		{name: "days ago", time: now.Add(-2 * 24 * time.Hour), expected: "2 days ago"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.FormatRelativeTime(testCase.time)
			if result != testCase.expected {
				t.Errorf("FormatRelativeTime() = %q, want %q", result, testCase.expected)
			}
		})
	}
}

// TestHistoryDown_IgnoredWhenNotBrowsing tests that down does nothing at historyIndex -1.
func TestHistoryDown_IgnoredWhenNotBrowsing(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetHistory(model, []string{"old prompt"})

	var updatedModel tea.Model = model

	// Down without first going up should do nothing
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportGetHistoryIndex(chatModel) != -1 {
		t.Error("expected history index to remain -1 when not browsing")
	}
}

// TestHistoryDown_IgnoredWhileStreaming tests that down arrow is ignored during streaming.
func TestHistoryDown_IgnoredWhileStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetHistory(model, []string{"old prompt"})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportGetHistoryIndex(chatModel) != -1 {
		t.Error("expected history index to remain -1 when streaming")
	}
}

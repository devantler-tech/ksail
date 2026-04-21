package chat_test

import (
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SessionMetadata.GetDisplayName ---

func TestGetDisplayName_LocalNameTakesPrecedence(t *testing.T) {
	t.Parallel()

	summary := "SDK summary"
	session := chat.SessionMetadata{
		Name: "My Custom Name",
		SDKMetadata: &copilot.SessionMetadata{
			Summary: &summary,
		},
	}

	assert.Equal(t, "My Custom Name", session.GetDisplayName())
}

func TestGetDisplayName_FallsBackToSDKSummary(t *testing.T) {
	t.Parallel()

	summary := "Generated summary"
	session := chat.SessionMetadata{
		SDKMetadata: &copilot.SessionMetadata{
			Summary: &summary,
		},
	}

	assert.Equal(t, "Generated summary", session.GetDisplayName())
}

func TestGetDisplayName_ReturnsUnnamedWhenNoNameOrSummary(t *testing.T) {
	t.Parallel()

	session := chat.SessionMetadata{}

	assert.Equal(t, "Unnamed", session.GetDisplayName())
}

func TestGetDisplayName_ReturnsUnnamedWhenSDKSummaryEmpty(t *testing.T) {
	t.Parallel()

	empty := ""
	session := chat.SessionMetadata{
		SDKMetadata: &copilot.SessionMetadata{
			Summary: &empty,
		},
	}

	assert.Equal(t, "Unnamed", session.GetDisplayName())
}

func TestGetDisplayName_ReturnsUnnamedWhenSDKMetadataNil(t *testing.T) {
	t.Parallel()

	session := chat.SessionMetadata{SDKMetadata: nil}

	assert.Equal(t, "Unnamed", session.GetDisplayName())
}

// --- validateSessionID ---

func TestValidateSessionID_EmptyReturnsError(t *testing.T) {
	t.Parallel()

	err := chat.ExportValidateSessionID("")

	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrSessionIDEmptyForTest)
}

func TestValidateSessionID_ValidIDs(t *testing.T) {
	t.Parallel()

	validIDs := []string{
		"abc",
		"ABC",
		"123",
		"abc-123",
		"abc_123",
		"session-id-2024",
		"a",
		"A1b2-c3_D4",
	}

	for _, id := range validIDs {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			err := chat.ExportValidateSessionID(id)

			require.NoError(t, err)
		})
	}
}

func TestValidateSessionID_InvalidCharsReturnError(t *testing.T) {
	t.Parallel()

	invalidIDs := []struct {
		name string
		id   string
	}{
		{"path traversal dots", "../../etc/passwd"},
		{"forward slash", "sess/ion"},
		{"backslash", `sess\ion`},
		{"space", "my session"},
		{"dot", "session.id"},
		{"at sign", "user@host"},
		{"dollar sign", "sess$ion"},
		{"null byte", "sess\x00ion"},
	}

	for _, tc := range invalidIDs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := chat.ExportValidateSessionID(tc.id)

			require.Error(t, err)
			require.ErrorIs(t, err, chat.ErrInvalidSessionIDForTest)
		})
	}
}

// --- isValidSessionIDChar ---

func TestIsValidSessionIDChar_AllowedChars(t *testing.T) {
	t.Parallel()

	allowedChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

	for _, c := range allowedChars {
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()

			assert.True(t, chat.ExportIsValidSessionIDChar(c),
				"expected %q to be a valid session ID char", string(c))
		})
	}
}

func TestIsValidSessionIDChar_DisallowedChars(t *testing.T) {
	t.Parallel()

	disallowedChars := []struct {
		name string
		char rune
	}{
		{"dot", '.'},
		{"forward slash", '/'},
		{"backslash", '\\'},
		{"space", ' '},
		{"at sign", '@'},
		{"hash", '#'},
		{"dollar sign", '$'},
		{"exclamation", '!'},
		{"percent", '%'},
		{"newline", '\n'},
		{"tab", '\t'},
		{"null byte", '\x00'},
	}

	for _, tc := range disallowedChars {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.False(t, chat.ExportIsValidSessionIDChar(tc.char),
				"expected %q to be an invalid session ID char", string(tc.char))
		})
	}
}

// --- GenerateSessionName ---

func TestGenerateSessionName_NoMessages(t *testing.T) {
	t.Parallel()

	result := chat.GenerateSessionName(nil)

	assert.Equal(t, "New Chat", result)
}

func TestGenerateSessionName_EmptyMessages(t *testing.T) {
	t.Parallel()

	result := chat.GenerateSessionName([]chat.MessageForTest{})

	assert.Equal(t, "New Chat", result)
}

func TestGenerateSessionName_OnlyAssistantMessages(t *testing.T) {
	t.Parallel()

	msgs := []chat.MessageForTest{
		chat.ExportNewAssistantMessage("Hello, how can I help?"),
	}

	result := chat.GenerateSessionName(msgs)

	assert.Equal(t, "New Chat", result)
}

func TestGenerateSessionName_UserMessageUsedAsName(t *testing.T) {
	t.Parallel()

	msgs := []chat.MessageForTest{
		chat.ExportNewUserMessage("How do I create a cluster?"),
	}

	result := chat.GenerateSessionName(msgs)

	assert.Equal(t, "How do I create a cluster?", result)
}

func TestGenerateSessionName_FirstUserMessageUsed(t *testing.T) {
	t.Parallel()

	msgs := []chat.MessageForTest{
		chat.ExportNewAssistantMessage("Hello"),
		chat.ExportNewUserMessage("First user message"),
		chat.ExportNewUserMessage("Second user message"),
	}

	result := chat.GenerateSessionName(msgs)

	assert.Equal(t, "First user message", result)
}

func TestGenerateSessionName_NewlineBecomesSpace(t *testing.T) {
	t.Parallel()

	msgs := []chat.MessageForTest{
		chat.ExportNewUserMessage("line one\nline two"),
	}

	result := chat.GenerateSessionName(msgs)

	assert.Equal(t, "line one line two", result)
}

func TestGenerateSessionName_TruncatesLongMessages(t *testing.T) {
	t.Parallel()

	// maxSessionNameLength = 40, truncatedNameSuffix = "..."
	longContent := "This is a very long message that exceeds the maximum name length limit set by the constant"
	msgs := []chat.MessageForTest{
		chat.ExportNewUserMessage(longContent),
	}

	result := chat.GenerateSessionName(msgs)

	assert.LessOrEqual(t, len([]rune(result)), 40, "truncated name should be at most 40 runes")
	assert.True(t, strings.HasSuffix(result, "..."), "truncated name should end with ...")
}

func TestGenerateSessionName_ExactlyMaxLength(t *testing.T) {
	t.Parallel()

	// Exactly 40 chars — should NOT be truncated
	content := "Exactly forty characters long message!!!"
	require.Len(t, []rune(content), 40)

	msgs := []chat.MessageForTest{
		chat.ExportNewUserMessage(content),
	}

	result := chat.GenerateSessionName(msgs)

	assert.Equal(t, content, result)
}

func TestGenerateSessionName_EmptyUserMessageSkipped(t *testing.T) {
	t.Parallel()

	msgs := []chat.MessageForTest{
		chat.ExportNewUserMessage(""),
		chat.ExportNewUserMessage("second non-empty"),
	}

	result := chat.GenerateSessionName(msgs)

	assert.Equal(t, "second non-empty", result)
}

// --- FormatRelativeTime ---

func TestFormatRelativeTime_JustNow(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-10 * time.Second)

	result := chat.FormatRelativeTime(ts)

	assert.Equal(t, "just now", result)
}

func TestFormatRelativeTime_OneMinAgo(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-1 * time.Minute)

	result := chat.FormatRelativeTime(ts)

	assert.Equal(t, "1 min ago", result)
}

func TestFormatRelativeTime_FewMinsAgo(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-15 * time.Minute)

	result := chat.FormatRelativeTime(ts)

	assert.Equal(t, "15 mins ago", result)
}

func TestFormatRelativeTime_OneHourAgo(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-1 * time.Hour)

	result := chat.FormatRelativeTime(ts)

	assert.Equal(t, "1 hour ago", result)
}

func TestFormatRelativeTime_FewHoursAgo(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-5 * time.Hour)

	result := chat.FormatRelativeTime(ts)

	assert.Equal(t, "5 hours ago", result)
}

func TestFormatRelativeTime_Yesterday(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-25 * time.Hour)

	result := chat.FormatRelativeTime(ts)

	assert.Equal(t, "yesterday", result)
}

func TestFormatRelativeTime_FewDaysAgo(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-3 * 24 * time.Hour)

	result := chat.FormatRelativeTime(ts)

	assert.Equal(t, "3 days ago", result)
}

func TestFormatRelativeTime_OldTimestampFormatsAsDate(t *testing.T) {
	t.Parallel()

	ts := time.Now().Add(-10 * 24 * time.Hour)

	result := chat.FormatRelativeTime(ts)

	// Should be formatted as "Jan 2" style date, not a relative string.
	assert.NotContains(t, result, "ago")
	assert.NotEqual(t, "just now", result)
}

// --- SaveSession / LoadSession / deleteLocalSession ---
//
// These tests modify the HOME environment variable and must NOT be run in parallel
// (t.Setenv panics if called after t.Parallel).

func TestSaveAndLoadSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	session := &chat.SessionMetadata{
		ID:   "test-session-1",
		Name: "Test Session",
	}

	err := chat.SaveSession(session, ".ksail-test")

	require.NoError(t, err)
	assert.NotZero(t, session.CreatedAt)
	assert.NotZero(t, session.UpdatedAt)

	loaded, err := chat.LoadSession("test-session-1", ".ksail-test")

	require.NoError(t, err)
	assert.Equal(t, "test-session-1", loaded.ID)
	assert.Equal(t, "Test Session", loaded.Name)
}

func TestSaveSession_SetsDefaultNameWhenEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	session := &chat.SessionMetadata{ID: "test-default-name"}

	err := chat.SaveSession(session, ".ksail-test")

	require.NoError(t, err)
	assert.Equal(t, "New Chat", session.Name)
}

func TestSaveSession_PreservesCreatedAt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	createdAt := time.Now().Add(-1 * time.Hour)
	session := &chat.SessionMetadata{
		ID:        "test-preserve-created",
		Name:      "Keep Created",
		CreatedAt: createdAt,
	}

	err := chat.SaveSession(session, ".ksail-test")

	require.NoError(t, err)
	assert.Equal(t, createdAt.Unix(), session.CreatedAt.Unix())
}

func TestSaveSession_InvalidIDReturnsError(t *testing.T) {
	t.Parallel()

	session := &chat.SessionMetadata{ID: ""}

	err := chat.SaveSession(session, ".ksail-test")

	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrSessionIDEmptyForTest)
}

func TestSaveSession_InvalidAppDirReturnsError(t *testing.T) {
	t.Parallel()

	session := &chat.SessionMetadata{ID: "valid-id", Name: "Test"}

	err := chat.SaveSession(session, "../sneaky")

	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrInvalidAppDirForTest)
}

func TestLoadSession_NonExistentReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := chat.LoadSession("nonexistent-id", ".ksail-test")

	require.Error(t, err)
}

func TestLoadSession_InvalidIDReturnsError(t *testing.T) {
	t.Parallel()

	_, err := chat.LoadSession("../invalid", ".ksail-test")

	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrInvalidSessionIDForTest)
}

func TestDeleteLocalSession_DeletesExistingSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	session := &chat.SessionMetadata{ID: "deletable-session", Name: "To Delete"}

	err := chat.SaveSession(session, ".ksail-test")
	require.NoError(t, err)

	err = chat.ExportDeleteLocalSession("deletable-session", ".ksail-test")
	require.NoError(t, err)

	_, err = chat.LoadSession("deletable-session", ".ksail-test")
	require.Error(t, err)
}

func TestDeleteLocalSession_NonExistentIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := chat.ExportDeleteLocalSession("does-not-exist", ".ksail-test")

	require.NoError(t, err)
}

func TestDeleteLocalSession_InvalidIDReturnsError(t *testing.T) {
	t.Parallel()

	err := chat.ExportDeleteLocalSession("", ".ksail-test")

	require.Error(t, err)
	require.ErrorIs(t, err, chat.ErrSessionIDEmptyForTest)
}

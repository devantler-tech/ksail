package chat_test

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ctrlCSessionID = "ctrlc-test-session"

// newCtrlCTestModel builds a chat model with a real session ID and one message so
// that saveCurrentSession() has something to persist when Ctrl+C is pressed.
func newCtrlCTestModel(t *testing.T) *chat.Model {
	t.Helper()

	params := newTestParams()
	params.Session = &copilot.Session{SessionID: ctrlCSessionID}

	model := chat.NewModel(params)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewUserMessage("hello"),
	})

	return model
}

// ctrlCSessionFile returns the on-disk path where newCtrlCTestModel's session is
// saved (under $HOME, which each subtest points at a fresh temp dir).
func ctrlCSessionFile(t *testing.T) string {
	t.Helper()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	return filepath.Join(home, ".ksail", "chat", "sessions", ctrlCSessionID+".json")
}

// ctrlCQuitCase is one UI state to enter before pressing Ctrl+C.
type ctrlCQuitCase struct {
	name  string
	setup func(m *chat.Model)
}

// ctrlCQuitCases enumerates the prompt plus every picker / permission / elicitation
// overlay that has its own Ctrl+C handler, so the regression guard covers them all.
func ctrlCQuitCases() []ctrlCQuitCase {
	return []ctrlCQuitCase{
		{name: "main prompt", setup: func(_ *chat.Model) {}},
		{name: "model picker", setup: func(m *chat.Model) {
			chat.ExportSetShowModelPicker(m, true)
		}},
		{name: "model picker filter mode", setup: func(m *chat.Model) {
			chat.ExportSetShowModelPicker(m, true)
			chat.ExportSetModelFilterActive(m, true)
		}},
		{name: "session picker", setup: func(m *chat.Model) {
			chat.ExportSetShowSessionPicker(m, true)
		}},
		{name: "session picker filter mode", setup: func(m *chat.Model) {
			chat.ExportSetShowSessionPicker(m, true)
			chat.ExportSetSessionFilterActive(m, true)
		}},
		{name: "reasoning picker", setup: func(m *chat.Model) {
			chat.ExportSetShowReasoningPicker(m, true)
		}},
		{name: "permission prompt", setup: func(m *chat.Model) {
			// Buffered so the handler's `response <- false` never blocks.
			resp := make(chan bool, 1)
			chat.ExportSetPendingPermission(m, "tool", "cmd", "args", resp)
		}},
		{name: "elicitation prompt", setup: func(m *chat.Model) {
			// Buffered so cancelElicitation's response send never blocks.
			resp := make(chan chat.ExportElicitationResponsePayload, 1)
			m.Update(chat.ExportElicitationRequestMsg{Message: "confirm?", Response: resp})
		}},
	}
}

// TestCtrlC_SavesSessionFromEveryUIState is the regression guard for #5045: Ctrl+C
// must route through the single handleQuit() helper — persisting the current session
// and setting quitting — identically from the main prompt and from every picker /
// permission / elicitation overlay. Before the fix, the overlays issued tea.Quit
// inline and silently dropped the session.
func TestCtrlC_SavesSessionFromEveryUIState(t *testing.T) {
	for _, testCase := range ctrlCQuitCases() {
		t.Run(testCase.name, func(t *testing.T) {
			// Fresh home per case so the saved file proves THIS Ctrl+C wrote it.
			// Set both HOME and USERPROFILE (os.UserHomeDir consults USERPROFILE on
			// Windows) to the same dir so the test is portable, matching the
			// internal/testutil/homeenv helper.
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("USERPROFILE", homeDir)

			model := newCtrlCTestModel(t)
			testCase.setup(model)

			require.NoFileExists(t, ctrlCSessionFile(t),
				"session file must not exist before Ctrl+C")

			updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

			chatModel, ok := updated.(*chat.Model)
			require.True(t, ok, "expected *chat.Model after Ctrl+C")

			assert.True(t, chat.ExportGetQuitting(chatModel),
				"Ctrl+C must set quitting from %q", testCase.name)
			assert.FileExists(t, ctrlCSessionFile(t),
				"Ctrl+C must save the current session (handleQuit -> saveCurrentSession) from %q",
				testCase.name)
		})
	}
}

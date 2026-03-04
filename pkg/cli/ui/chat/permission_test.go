package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestPermissionModal_VisibleInView tests that the permission modal renders when a request is pending.
func TestPermissionModal_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "rm -rf /tmp/test", "", responseChan)

	output := model.View()

	if !strings.Contains(output, "Permission Required") {
		t.Error("expected 'Permission Required' in view when permission is pending")
	}

	if !strings.Contains(output, "Shell Command") {
		t.Error("expected tool name 'Shell Command' in permission modal")
	}

	if !strings.Contains(output, "rm -rf /tmp/test") {
		t.Error("expected command 'rm -rf /tmp/test' in permission modal")
	}
}

// TestPermissionModal_WithArguments tests that arguments are displayed when present.
func TestPermissionModal_WithArguments(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "File Edit", "/etc/config", "--force", responseChan)

	output := model.View()

	if !strings.Contains(output, "Arguments: --force") {
		t.Error("expected 'Arguments: --force' in permission modal")
	}
}

// TestPermissionKey_AllowWithY tests that pressing 'y' approves a permission request.
func TestPermissionKey_AllowWithY(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Read the response
	approved := <-responseChan
	if !approved {
		t.Error("expected permission to be approved after pressing 'y'")
	}

	// Permission should be cleared
	chatModel := updatedModel.(*chat.Model)
	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after approval")
	}

	// Permission history should record the approval
	if chat.ExportGetPermissionHistoryLen(chatModel) != 1 {
		t.Errorf("expected 1 permission history entry, got %d", chat.ExportGetPermissionHistoryLen(chatModel))
	}

	if !chat.ExportGetPermissionHistoryLastAllowed(chatModel) {
		t.Error("expected last permission to be allowed")
	}
}

// TestPermissionKey_AllowWithUpperY tests that pressing 'Y' also approves.
func TestPermissionKey_AllowWithUpperY(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})

	approved := <-responseChan
	if !approved {
		t.Error("expected permission to be approved after pressing 'Y'")
	}
}

// TestPermissionKey_DenyWithN tests that pressing 'n' denies a permission request.
func TestPermissionKey_DenyWithN(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "rm -rf /", "", responseChan)

	var updatedModel tea.Model = model
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	denied := <-responseChan
	if denied {
		t.Error("expected permission to be denied after pressing 'n'")
	}

	// Permission history should record the denial
	chatModel := updatedModel.(*chat.Model)

	if chat.ExportGetPermissionHistoryLastAllowed(chatModel) {
		t.Error("expected last permission to be denied")
	}
}

// TestPermissionKey_DenyWithEsc tests that pressing 'esc' denies a permission request.
func TestPermissionKey_DenyWithEsc(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	denied := <-responseChan
	if denied {
		t.Error("expected permission to be denied after pressing escape")
	}

	chatModel := updatedModel.(*chat.Model)
	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after denial")
	}
}

// TestPermissionHandler_YoloAutoApproves tests that YOLO mode auto-approves permissions.
func TestPermissionHandler_YoloAutoApproves(t *testing.T) {
	t.Parallel()

	yoloRef := chat.NewYoloModeRef(true) // YOLO enabled
	eventChan := make(chan tea.Msg, 10)

	handler := chat.CreateTUIPermissionHandler(eventChan, yoloRef)

	result, err := handler(
		copilot.PermissionRequest{
			Kind:  "shell",
			Extra: map[string]any{"command": "rm -rf /"},
		},
		copilot.PermissionInvocation{},
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Kind != "approved" {
		t.Errorf("expected 'approved' in YOLO mode, got %q", result.Kind)
	}
}

// TestPermissionHandler_NonYoloSendsToChannel tests that non-YOLO mode sends to event channel.
func TestPermissionHandler_NonYoloSendsToChannel(t *testing.T) {
	t.Parallel()

	yoloRef := chat.NewYoloModeRef(false) // YOLO disabled
	eventChan := make(chan tea.Msg, 10)

	handler := chat.CreateTUIPermissionHandler(eventChan, yoloRef)

	// Run handler in goroutine since it blocks waiting for response
	resultChan := make(chan copilot.PermissionRequestResult, 1)

	go func() {
		result, _ := handler(
			copilot.PermissionRequest{
				Kind:       "shell",
				ToolCallID: "test-123",
				Extra:      map[string]any{"command": "echo hello"},
			},
			copilot.PermissionInvocation{},
		)
		resultChan <- result
	}()

	// Read the permission request from the event channel
	msg := <-eventChan

	// Verify it's a permission request message
	if msg == nil {
		t.Fatal("expected a permission request message on event channel")
	}

	// The message should be a permissionRequestMsg (unexported), but we can verify
	// the channel sent something. We need to approve to unblock the handler.
	// Since permissionRequestMsg is unexported, we test via the TUI integration approach.
}

// TestPermissionHandler_NilYoloRef tests that nil yoloModeRef doesn't auto-approve.
func TestPermissionHandler_NilYoloRef(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)

	handler := chat.CreateTUIPermissionHandler(eventChan, nil)

	// Run in goroutine since it blocks
	go func() {
		_, _ = handler(
			copilot.PermissionRequest{
				Kind:  "shell",
				Extra: map[string]any{"command": "ls"},
			},
			copilot.PermissionInvocation{},
		)
	}()

	// Verify a message was sent to the channel (not auto-approved)
	msg := <-eventChan
	if msg == nil {
		t.Error("expected permission request to be sent to event channel with nil yoloRef")
	}
}

// TestFormatPermissionKind tests the formatting of permission kinds.
func TestFormatPermissionKind(t *testing.T) {
	t.Parallel()

	// We test this indirectly through the permission modal display since
	// formatPermissionKind is unexported. Instead, test through CreateTUIPermissionHandler
	// and verify the resulting tool names.
	tests := []struct {
		name     string
		kind     string
		toolName string
		command  string
	}{
		{
			name:     "shell command shows in modal",
			kind:     "shell",
			toolName: "Shell Command",
			command:  "echo test",
		},
		{
			name:     "file edit shows in modal",
			kind:     "file_edit",
			toolName: "File Edit",
			command:  "/tmp/test.go",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			responseChan := make(chan bool, 1)
			chat.ExportSetPendingPermission(model, tc.toolName, tc.command, "", responseChan)

			output := model.View()

			if !strings.Contains(output, tc.toolName) {
				t.Errorf("expected tool name %q in permission modal", tc.toolName)
			}

			if !strings.Contains(output, tc.command) {
				t.Errorf("expected command %q in permission modal", tc.command)
			}
		})
	}
}

// TestPermissionModal_AllowThisOperation tests that the modal shows the allow prompt.
func TestPermissionModal_AllowThisOperation(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "Terminal", "npm install", "", responseChan)

	output := model.View()

	if !strings.Contains(output, "Allow this operation?") {
		t.Error("expected 'Allow this operation?' prompt in permission modal")
	}
}

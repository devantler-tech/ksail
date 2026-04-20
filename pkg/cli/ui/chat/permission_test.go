package chat_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
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

	// Read the response with timeout to avoid hanging on regression
	select {
	case approved := <-responseChan:
		if !approved {
			t.Error("expected permission to be approved after pressing 'y'")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after pressing 'y'")
	}

	// Permission should be cleared
	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after approval")
	}

	// Permission history should record the approval
	if chat.ExportGetPermissionHistoryLen(chatModel) != 1 {
		t.Errorf(
			"expected 1 permission history entry, got %d",
			chat.ExportGetPermissionHistoryLen(chatModel),
		)
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

	select {
	case approved := <-responseChan:
		if !approved {
			t.Error("expected permission to be approved after pressing 'Y'")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after pressing 'Y'")
	}

	// Verify type assertion succeeds (no further checks needed — AllowWithY covers history)
	_, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
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

	select {
	case denied := <-responseChan:
		if denied {
			t.Error("expected permission to be denied after pressing 'n'")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after pressing 'n'")
	}

	// Permission history should record the denial
	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

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

	select {
	case denied := <-responseChan:
		if denied {
			t.Error("expected permission to be denied after pressing escape")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after pressing escape")
	}

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after denial")
	}
}

// TestPermissionHandler_AutopilotAutoApproves tests that Autopilot mode auto-approves permissions.
func TestPermissionHandler_AutopilotAutoApproves(t *testing.T) {
	t.Parallel()

	chatModeRef := chat.NewChatModeRef(chat.AutopilotMode) // Autopilot enabled
	eventChan := make(chan tea.Msg, 10)

	handler := chat.CreateTUIPermissionHandler(eventChan, chatModeRef)

	result, err := handler(
		copilot.PermissionRequest{
			Kind:            copilot.PermissionRequestKindShell,
			FullCommandText: new("rm -rf /"),
		},
		copilot.PermissionInvocation{},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Kind != copilot.PermissionRequestResultKindApproved {
		t.Errorf("expected 'approved' in Autopilot mode, got %q", result.Kind)
	}
}

// TestPermissionHandler_InteractiveSendsToChannel tests that Interactive mode sends to event channel
// and correctly returns the result when the TUI approves the request.
func TestPermissionHandler_InteractiveSendsToChannel(t *testing.T) {
	t.Parallel()

	chatModeRef := chat.NewChatModeRef(chat.InteractiveMode) // Interactive mode
	eventChan := make(chan tea.Msg, 10)

	handler := chat.CreateTUIPermissionHandler(eventChan, chatModeRef)

	// Run handler in goroutine since it blocks waiting for response
	resultChan := make(chan copilot.PermissionRequestResult, 1)

	go func() {
		result, _ := handler(
			copilot.PermissionRequest{
				Kind:            copilot.PermissionRequestKindShell,
				ToolCallID:      new("test-123"),
				FullCommandText: new("echo hello"),
			},
			copilot.PermissionInvocation{},
		)
		resultChan <- result
	}()

	// Read the permission request from the event channel with timeout
	var msg tea.Msg

	select {
	case msg = <-eventChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission request on event channel")
	}

	if msg == nil {
		t.Fatal("expected a permission request message on event channel")
	}

	// Feed the message through a TUI model, then press 'y' to approve
	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(msg)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Verify the handler goroutine returns "approved"
	select {
	case result := <-resultChan:
		if result.Kind != copilot.PermissionRequestResultKindApproved {
			t.Errorf("expected 'approved', got %q", result.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission handler result")
	}

	// Verify the model cleared the pending permission
	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after approval")
	}
}

// TestPermissionHandler_NilChatModeRef tests that nil chatModeRef sends to event channel
// and correctly returns "denied-interactively-by-user" when the TUI denies the request.
func TestPermissionHandler_NilChatModeRef(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)

	handler := chat.CreateTUIPermissionHandler(eventChan, nil)

	// Run handler in goroutine since it blocks waiting for response
	resultChan := make(chan copilot.PermissionRequestResult, 1)

	go func() {
		result, _ := handler(
			copilot.PermissionRequest{
				Kind:            copilot.PermissionRequestKindShell,
				FullCommandText: new("ls"),
			},
			copilot.PermissionInvocation{},
		)
		resultChan <- result
	}()

	// Read the permission request from the event channel (not auto-approved)
	var msg tea.Msg

	select {
	case msg = <-eventChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission request on event channel")
	}

	if msg == nil {
		t.Fatal("expected permission request to be sent to event channel with nil chatModeRef")
	}

	// Feed the message through a TUI model, then press 'esc' to deny
	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(msg)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Verify the handler goroutine returns "denied-interactively-by-user"
	select {
	case result := <-resultChan:
		if result.Kind != copilot.PermissionRequestResultKindDeniedInteractivelyByUser {
			t.Errorf("expected 'denied-interactively-by-user', got %q", result.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission handler result")
	}

	// Verify the model cleared the pending permission
	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after denial")
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

// TestPermissionModal_AllowAlwaysSwitchesToAutopilot tests that pressing 'a' on the
// permission prompt switches to Autopilot mode and approves the request.
func TestPermissionModal_AllowAlwaysSwitchesToAutopilot(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetChatMode(model, chat.InteractiveMode)

	responseChan := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "Terminal", "echo hello", "", responseChan)

	// Press 'a' to allow always
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	// Verify mode switched to Autopilot
	if chat.ExportGetChatMode(chatModel) != chat.AutopilotMode {
		t.Errorf("expected AutopilotMode after 'a', got %v", chat.ExportGetChatMode(chatModel))
	}

	// Verify the permission was approved
	select {
	case approved := <-responseChan:
		if !approved {
			t.Error("expected permission to be approved after 'a'")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission response")
	}

	// Verify pending permission was cleared
	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after 'a'")
	}
}

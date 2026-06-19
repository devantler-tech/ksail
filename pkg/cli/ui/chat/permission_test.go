package chat_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

// TestPermissionModal_VisibleInView tests that the permission modal renders when a request is pending.
func TestPermissionModal_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
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
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
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
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Read the response with timeout to avoid hanging on regression
	select {
	case outcome := <-responseChan:
		if outcome != chat.OutcomeApproveOnceForTest {
			t.Errorf("expected approve-once after pressing 'y', got %v", outcome)
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
}

// TestPermissionKey_AllowWithUpperY tests that pressing 'Y' also approves.
func TestPermissionKey_AllowWithUpperY(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})

	select {
	case outcome := <-responseChan:
		if outcome != chat.OutcomeApproveOnceForTest {
			t.Errorf("expected approve-once after pressing 'Y', got %v", outcome)
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
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "rm -rf /", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	select {
	case outcome := <-responseChan:
		if outcome != chat.OutcomeRejectForTest {
			t.Errorf("expected reject after pressing 'n', got %v", outcome)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after pressing 'n'")
	}

	// Permission should be cleared
	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after denial")
	}
}

// TestPermissionKey_DenyWithEsc tests that pressing 'esc' denies a permission request.
func TestPermissionKey_DenyWithEsc(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	select {
	case outcome := <-responseChan:
		if outcome != chat.OutcomeRejectForTest {
			t.Errorf("expected reject after pressing escape, got %v", outcome)
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
		&copilot.PermissionRequestShell{
			FullCommandText: "rm -rf /",
		},
		copilot.PermissionInvocation{},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Kind() != rpc.PermissionDecisionKindApproveOnce {
		t.Errorf("expected 'approve-once' in Autopilot mode, got %q", result.Kind())
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
	resultChan := make(chan rpc.PermissionDecision, 1)

	go func() {
		result, _ := handler(
			&copilot.PermissionRequestShell{
				ToolCallID:      new("test-123"),
				FullCommandText: "echo hello",
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
		if result.Kind() != rpc.PermissionDecisionKindApproveOnce {
			t.Errorf("expected 'approve-once', got %q", result.Kind())
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
	resultChan := make(chan rpc.PermissionDecision, 1)

	go func() {
		result, _ := handler(
			&copilot.PermissionRequestShell{
				FullCommandText: "ls",
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

	// Verify the handler goroutine returns rejected
	select {
	case result := <-resultChan:
		if result.Kind() != rpc.PermissionDecisionKindReject {
			t.Errorf(
				"expected %q, got %q",
				rpc.PermissionDecisionKindReject,
				result.Kind(),
			)
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
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
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

	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
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

	// Verify the permission was approved (once; Autopilot handles the rest)
	select {
	case outcome := <-responseChan:
		if outcome != chat.OutcomeApproveOnceForTest {
			t.Errorf("expected approve-once after 'a', got %v", outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission response")
	}

	// Verify pending permission was cleared
	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after 'a'")
	}
}

// TestPermissionModal_SessionHintShownWhenApplicable verifies the "s" (session)
// option is advertised only when the request supports session approval.
func TestPermissionModal_SessionHintShownWhenApplicable(t *testing.T) {
	t.Parallel()

	// Applicable: hint mentions the session option.
	withSession := chat.NewModel(newTestParams())
	respA := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermissionSession(withSession, "Shell Command", "ls", "", respA)

	if out := withSession.View(); !strings.Contains(out, "allow for session") {
		t.Error("expected 'allow for session' hint when session approval is applicable")
	}

	// Not applicable: hint omits the session option.
	noSession := chat.NewModel(newTestParams())
	respB := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermission(noSession, "Shell Command", "ls", "", respB)

	if out := noSession.View(); strings.Contains(out, "allow for session") {
		t.Error("did not expect 'allow for session' hint when session approval is not applicable")
	}
}

// TestPermissionKey_SessionApprovalWithS verifies pressing 's' yields an
// approve-session outcome when the request supports it.
func TestPermissionKey_SessionApprovalWithS(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermissionSession(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	select {
	case outcome := <-responseChan:
		if outcome != chat.OutcomeApproveSessionForTest {
			t.Errorf("expected approve-session after pressing 's', got %v", outcome)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after pressing 's'")
	}

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected pending permission to be cleared after session approval")
	}
}

// TestPermissionKey_SFallsBackToApproveOnce verifies pressing 's' on a request
// that does not support session approval is treated as a one-time approval.
func TestPermissionKey_SFallsBackToApproveOnce(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionOutcomeForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	select {
	case outcome := <-responseChan:
		if outcome != chat.OutcomeApproveOnceForTest {
			t.Errorf("expected approve-once fallback after pressing 's', got %v", outcome)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after pressing 's'")
	}
}

// TestPermissionHandler_SessionApprovalShell verifies the handler returns an
// approve-for-session decision for a Shell request when the user presses 's'.
func TestPermissionHandler_SessionApprovalShell(t *testing.T) {
	t.Parallel()

	chatModeRef := chat.NewChatModeRef(chat.InteractiveMode)
	eventChan := make(chan tea.Msg, 10)

	handler := chat.CreateTUIPermissionHandler(eventChan, chatModeRef)

	resultChan := make(chan rpc.PermissionDecision, 1)

	go func() {
		result, _ := handler(
			&copilot.PermissionRequestShell{
				ToolCallID:              new("shell-session-1"),
				FullCommandText:         "ls -la",
				CanOfferSessionApproval: true,
				Commands: []copilot.PermissionRequestShellCommand{
					{Identifier: "ls"},
				},
			},
			copilot.PermissionInvocation{},
		)
		resultChan <- result
	}()

	var msg tea.Msg

	select {
	case msg = <-eventChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission request on event channel")
	}

	// Feed the request through the TUI and press 's' for session approval.
	model := chat.NewModel(newTestParams())

	updated, _ := model.Update(msg)
	_, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	select {
	case result := <-resultChan:
		assertShellCommandsApproval(t, result, []string{"ls"})
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for permission handler result")
	}
}

// assertShellCommandsApproval verifies a decision is an approve-for-session decision
// carrying a Commands approval with the expected command identifiers.
func assertShellCommandsApproval(
	t *testing.T,
	result rpc.PermissionDecision,
	wantIdentifiers []string,
) {
	t.Helper()

	if result.Kind() != rpc.PermissionDecisionKindApproveForSession {
		t.Fatalf(
			"expected %q, got %q",
			rpc.PermissionDecisionKindApproveForSession,
			result.Kind(),
		)
	}

	decision, isSession := result.(*rpc.PermissionDecisionApproveForSession)
	if !isSession {
		t.Fatalf("expected *PermissionDecisionApproveForSession, got %T", result)
	}

	commands, isCommands := decision.Approval.(rpc.PermissionDecisionApproveForSessionApprovalCommands)
	if !isCommands {
		t.Fatalf("expected Commands approval, got %T", decision.Approval)
	}

	if len(commands.CommandIdentifiers) != len(wantIdentifiers) {
		t.Fatalf("expected %v, got %v", wantIdentifiers, commands.CommandIdentifiers)
	}

	for i, id := range wantIdentifiers {
		if commands.CommandIdentifiers[i] != id {
			t.Errorf("identifier[%d] = %q, want %q", i, commands.CommandIdentifiers[i], id)
		}
	}
}

// TestCanOfferSessionApproval verifies which request kinds advertise session approval.
func TestCanOfferSessionApproval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request copilot.PermissionRequest
		want    bool
	}{
		{
			name:    "shell offered",
			request: &copilot.PermissionRequestShell{CanOfferSessionApproval: true},
			want:    true,
		},
		{
			name:    "shell not offered",
			request: &copilot.PermissionRequestShell{CanOfferSessionApproval: false},
			want:    false,
		},
		{
			name:    "write offered",
			request: &copilot.PermissionRequestWrite{CanOfferSessionApproval: true},
			want:    true,
		},
		{
			name:    "mcp unsupported",
			request: &copilot.PermissionRequestMCP{ToolName: "x"},
			want:    false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := chat.ExportCanOfferSessionApproval(testCase.request); got != testCase.want {
				t.Errorf("canOfferSessionApproval = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestSessionApproval_Shell verifies the Shell request maps to a Commands approval
// built from the parsed command identifiers (empty identifiers are dropped).
func TestSessionApproval_Shell(t *testing.T) {
	t.Parallel()

	approval := chat.ExportSessionApproval(&copilot.PermissionRequestShell{
		Commands: []copilot.PermissionRequestShellCommand{
			{Identifier: "git"},
			{Identifier: ""},
			{Identifier: "ls"},
		},
	})

	commands, ok := approval.(rpc.PermissionDecisionApproveForSessionApprovalCommands)
	if !ok {
		t.Fatalf("expected Commands approval, got %T", approval)
	}

	want := []string{"git", "ls"}
	if len(commands.CommandIdentifiers) != len(want) {
		t.Fatalf("expected %v, got %v", want, commands.CommandIdentifiers)
	}

	for i, id := range want {
		if commands.CommandIdentifiers[i] != id {
			t.Errorf("identifier[%d] = %q, want %q", i, commands.CommandIdentifiers[i], id)
		}
	}
}

// TestSessionApproval_Write verifies the Write request maps to the empty Write approval.
func TestSessionApproval_Write(t *testing.T) {
	t.Parallel()

	approval := chat.ExportSessionApproval(&copilot.PermissionRequestWrite{
		FileName: "main.go",
	})

	if approval.Kind() != rpc.PermissionDecisionApproveForSessionApprovalKindWrite {
		t.Errorf(
			"expected %q, got %q",
			rpc.PermissionDecisionApproveForSessionApprovalKindWrite,
			approval.Kind(),
		)
	}
}

// TestSessionApproval_UnsupportedReturnsNil verifies unsupported kinds yield no approval.
func TestSessionApproval_UnsupportedReturnsNil(t *testing.T) {
	t.Parallel()

	approval := chat.ExportSessionApproval(&copilot.PermissionRequestMCP{ToolName: "x"})
	if approval != nil {
		t.Errorf("expected nil approval for unsupported kind, got %T", approval)
	}
}

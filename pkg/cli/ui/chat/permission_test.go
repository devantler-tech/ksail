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
	responseChan := make(chan chat.PermissionResponseForTest, 1)
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
	responseChan := make(chan chat.PermissionResponseForTest, 1)
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
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// Read the response with timeout to avoid hanging on regression
	select {
	case resp := <-responseChan:
		if !chat.ExportPermissionResponseApproved(resp) {
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
}

// TestPermissionKey_AllowWithUpperY tests that pressing 'Y' also approves.
func TestPermissionKey_AllowWithUpperY(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})

	select {
	case resp := <-responseChan:
		if !chat.ExportPermissionResponseApproved(resp) {
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

// TestPermissionKey_DenyWithN tests that pressing 'n' opens the optional-reason input and that
// confirming with Enter (no reason) denies without feedback.
func TestPermissionKey_DenyWithN(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "rm -rf /", "", responseChan)

	var updatedModel tea.Model = model

	// 'n' opens the reason input; nothing is sent yet.
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	chatModel := requireModel(t, updatedModel)

	if !chat.ExportIsPermissionDenyInput(chatModel) {
		t.Error("expected deny-reason input to be active after pressing 'n'")
	}

	if len(responseChan) != 0 {
		t.Error("expected no response to be sent before confirming the denial")
	}

	// Enter confirms the denial without a reason.
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-responseChan:
		if chat.ExportPermissionResponseApproved(resp) {
			t.Error("expected permission to be denied")
		}

		if fb := chat.ExportPermissionResponseFeedback(resp); fb != "" {
			t.Errorf("expected no feedback, got %q", fb)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after confirming denial")
	}

	if chat.ExportHasPendingPermission(requireModel(t, updatedModel)) {
		t.Error("expected pending permission to be cleared after denial")
	}
}

// TestPermissionKey_DenyWithReason tests that a typed reason is forwarded as denial feedback.
func TestPermissionKey_DenyWithReason(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "rm -rf /", "", responseChan)

	var updatedModel tea.Model = model

	// Open the reason input, type a reason, then confirm.
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updatedModel, _ = updatedModel.Update(
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("too risky")},
	)

	if got := chat.ExportPermissionDenyValue(requireModel(t, updatedModel)); got != "too risky" {
		t.Errorf("expected deny input value %q, got %q", "too risky", got)
	}

	_, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-responseChan:
		if chat.ExportPermissionResponseApproved(resp) {
			t.Error("expected permission to be denied")
		}

		if fb := chat.ExportPermissionResponseFeedback(resp); fb != "too risky" {
			t.Errorf("expected feedback %q, got %q", "too risky", fb)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for permission response after confirming denial")
	}
}

// TestPermissionKey_DenyInputBackspace tests that backspace edits the reason input.
func TestPermissionKey_DenyInputBackspace(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "rm -rf /", "", responseChan)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("noo")})
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := chat.ExportPermissionDenyValue(requireModel(t, updatedModel)); got != "no" {
		t.Errorf("expected deny input value %q after backspace, got %q", "no", got)
	}
}

// TestPermissionKey_DenyInputEscCancels tests that Esc cancels reason entry and returns to the
// allow/deny prompt without sending a response.
func TestPermissionKey_DenyInputEscCancels(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	// Open reason input, then cancel with Esc.
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	chatModel := requireModel(t, updatedModel)

	if chat.ExportIsPermissionDenyInput(chatModel) {
		t.Error("expected deny-reason input to be cancelled after Esc")
	}

	if !chat.ExportHasPendingPermission(chatModel) {
		t.Error("expected permission prompt to remain active after cancelling reason entry")
	}

	if len(responseChan) != 0 {
		t.Error("expected no response to be sent when cancelling reason entry")
	}
}

// TestPermissionKey_DenyWithEsc tests that pressing 'esc' opens the optional-reason input and
// confirming with Enter denies the request.
func TestPermissionKey_DenyWithEsc(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Shell Command", "ls", "", responseChan)

	var updatedModel tea.Model = model

	// 'esc' opens the reason input; Enter confirms the denial.
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-responseChan:
		if chat.ExportPermissionResponseApproved(resp) {
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

	// Feed the message through a TUI model, then press 'esc' to open the reason input and
	// Enter to confirm the denial.
	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(msg)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

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
	responseChan := make(chan chat.PermissionResponseForTest, 1)
	chat.ExportSetPendingPermission(model, "Terminal", "npm install", "", responseChan)

	output := model.View()

	if !strings.Contains(output, "Allow this operation?") {
		t.Error("expected 'Allow this operation?' prompt in permission modal")
	}
}

// assertPermissionArguments runs a table of permissionArguments cases.
func assertPermissionArguments(t *testing.T, tests []struct {
	name    string
	request copilot.PermissionRequest
	want    string
},
) {
	t.Helper()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := chat.ExportPermissionArguments(testCase.request)
			if got != testCase.want {
				t.Errorf("permissionArguments() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestPermissionArguments_Shell tests shell-warning extraction for permission requests.
func TestPermissionArguments_Shell(t *testing.T) {
	t.Parallel()

	assertPermissionArguments(t, []struct {
		name    string
		request copilot.PermissionRequest
		want    string
	}{
		{
			name: "shell with warning",
			request: &copilot.PermissionRequestShell{
				FullCommandText: "rm -rf /",
				Warning:         new("dangerous"),
			},
			want: "⚠ dangerous",
		},
		{
			name: "shell without warning",
			request: &copilot.PermissionRequestShell{
				FullCommandText: "ls",
			},
			want: "",
		},
		{
			name:    "read has no extra context",
			request: &copilot.PermissionRequestRead{Path: "/etc/config.yaml"},
			want:    "",
		},
	})
}

// TestPermissionArguments_MCPAndWrite tests MCP server and write new-file extraction.
func TestPermissionArguments_MCPAndWrite(t *testing.T) {
	t.Parallel()

	assertPermissionArguments(t, []struct {
		name    string
		request copilot.PermissionRequest
		want    string
	}{
		{
			name: "mcp with server name",
			request: &copilot.PermissionRequestMCP{
				ServerName: "ksail",
				ToolName:   "cluster_create",
			},
			want: "Server: ksail",
		},
		{
			name: "mcp read-only",
			request: &copilot.PermissionRequestMCP{
				ServerName: "ksail",
				ToolName:   "cluster_list",
				ReadOnly:   true,
			},
			want: "Server: ksail (read-only)",
		},
		{
			name: "write new file",
			request: &copilot.PermissionRequestWrite{
				FileName:        "/tmp/output.txt",
				NewFileContents: new("hello"),
			},
			want: "New file",
		},
		{
			name: "write existing file",
			request: &copilot.PermissionRequestWrite{
				FileName: "/tmp/output.txt",
			},
			want: "",
		},
	})
}

// TestPermissionModal_AllowAlwaysSwitchesToAutopilot tests that pressing 'a' on the
// permission prompt switches to Autopilot mode and approves the request.
func TestPermissionModal_AllowAlwaysSwitchesToAutopilot(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetChatMode(model, chat.InteractiveMode)

	responseChan := make(chan chat.PermissionResponseForTest, 1)
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
	case resp := <-responseChan:
		if !chat.ExportPermissionResponseApproved(resp) {
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

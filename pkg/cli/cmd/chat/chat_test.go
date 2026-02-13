package chat_test

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/chat"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
)

const (
	successResult = "success"
	failureResult = "failure"
)

// modeTestCase defines a test case for mode-based tool execution.
type modeTestCase struct {
	name              string
	chatMode          chatui.ChatMode
	expectToolCalled  bool
	expectResultType  string
	expectMsgContains []string
}

// createTestTool creates a test tool that tracks whether it was called.
func createTestTool(called *bool) copilot.Tool {
	return copilot.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			*called = true

			return copilot.ToolResult{
				TextResultForLLM: "Tool executed successfully",
				ResultType:       successResult,
			}, nil
		},
	}
}

// TestModeToolExecution verifies agent/plan/ask mode tool execution behavior using table-driven tests.
func TestModeToolExecution(t *testing.T) {
	t.Parallel()

	tests := []modeTestCase{
		{
			name:             "AgentModeAllowsExecution",
			chatMode:         chatui.AgentMode,
			expectToolCalled: true,
			expectResultType: successResult,
		},
		{
			name:             "PlanModeBlocksExecution",
			chatMode:         chatui.PlanMode,
			expectToolCalled: false,
			expectResultType: "failure",
			expectMsgContains: []string{
				"Tool execution blocked - currently in Plan mode",
				"Switch to Agent mode",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runModeTestCase(t, testCase)
		})
	}
}

func runModeTestCase(t *testing.T, testCase modeTestCase) {
	t.Helper()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(testCase.chatMode)

	toolCalled := false
	testTool := createTestTool(&toolCalled)

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{testTool}, eventChan, chatModeRef, nil, nil,
	)

	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test1",
		ToolName:   "test_tool",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if toolCalled != testCase.expectToolCalled {
		t.Errorf("Expected toolCalled=%v, got %v", testCase.expectToolCalled, toolCalled)
	}

	if result.ResultType != testCase.expectResultType {
		t.Errorf("Expected result type %s, got %s", testCase.expectResultType, result.ResultType)
	}

	for _, msg := range testCase.expectMsgContains {
		if !strings.Contains(result.TextResultForLLM, msg) {
			t.Errorf("Expected message to contain %q, got: %s", msg, result.TextResultForLLM)
		}
	}
}

// TestModeToggle verifies that toggling between modes works correctly.
func TestModeToggle(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.PlanMode) // Start in plan mode

	toolCalled := false
	testTool := copilot.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			toolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Tool executed successfully",
				ResultType:       successResult,
			}, nil
		},
	}

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{testTool}, eventChan, chatModeRef, nil, nil,
	)
	wrappedTool := wrappedTools[0]

	// Phase 1: Plan mode should block
	result, _ := wrappedTool.Handler(copilot.ToolInvocation{
		ToolCallID: "test1", ToolName: "test_tool",
	})

	if toolCalled {
		t.Error("Tool should not be called in plan mode")
	}

	if result.ResultType != failureResult {
		t.Errorf("Expected failure in plan mode, got %s", result.ResultType)
	}

	// Phase 2: Toggle to agent mode - should execute
	toolCalled = false

	chatModeRef.SetMode(chatui.AgentMode)

	result, _ = wrappedTool.Handler(copilot.ToolInvocation{
		ToolCallID: "test2", ToolName: "test_tool",
	})

	if !toolCalled {
		t.Error("Tool should be called after toggling to agent mode")
	}

	if result.ResultType != successResult {
		t.Errorf("Expected success after toggle, got %s", result.ResultType)
	}
}

// TestPlanModeBlocksMutableTools verifies that edit tools are blocked in plan mode.
func TestPlanModeBlocksMutableTools(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.PlanMode) // Start in plan mode

	mutableToolCalled := false
	mutableTool := copilot.Tool{
		Name:        "ksail_cluster_create",
		Description: "Create a cluster",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			mutableToolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Cluster created",
				ResultType:       successResult,
			}, nil
		},
	}

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{mutableTool}, eventChan, chatModeRef, nil, nil,
	)

	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_mutable",
		ToolName:   "ksail_cluster_create",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if mutableToolCalled {
		t.Error("Edit tool should not have been called in plan mode")
	}

	if result.ResultType != failureResult {
		t.Errorf("Expected failure for blocked edit tool, got %s", result.ResultType)
	}

	if !strings.Contains(result.TextResultForLLM, "Tool execution blocked") {
		t.Error("Expected blocking message for edit tool in plan mode")
	}
}

// createTestWriteTool creates a write tool for permission testing.
func createTestWriteTool(toolName string, toolCalled *bool) copilot.Tool {
	return copilot.Tool{
		Name:        toolName,
		Description: "A write tool that requires permission",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			*toolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Write operation completed",
				ResultType:       "success",
			}, nil
		},
	}
}

// createToolMetadata creates metadata for a tool with permission requirements.
func createToolMetadata(
	toolName string,
) map[string]toolgen.ToolDefinition {
	return map[string]toolgen.ToolDefinition{
		toolName: {
			Name:               toolName,
			RequiresPermission: true,
		},
	}
}

// createToolMetadataWithForce creates metadata for a tool with permission and force support.
func createToolMetadataWithForce(
	toolName string,
) map[string]toolgen.ToolDefinition {
	return map[string]toolgen.ToolDefinition{
		toolName: {
			Name:               toolName,
			RequiresPermission: true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"force": map[string]any{
						"type":        "boolean",
						"description": "Skip confirmation prompts",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Cluster name",
					},
				},
			},
		},
	}
}

// waitForPermissionRequestAndRespond waits for a permission request and responds with the given value.
func waitForPermissionRequestAndRespond(
	t *testing.T,
	eventChan chan tea.Msg,
	expectedToolName string,
	approved bool,
) {
	t.Helper()

	select {
	case msg := <-eventChan:
		permReq, ok := msg.(chatui.PermissionRequestMsg)
		if !ok {
			t.Fatalf("Expected PermissionRequestMsg, got %T", msg)
		}

		if permReq.ToolName != expectedToolName {
			t.Errorf("Expected tool name '%s', got '%s'", expectedToolName, permReq.ToolName)
		}

		permReq.Response <- approved

	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for permission request")
	}
}

// TestWriteToolSendsPermissionRequest verifies that tools with RequiresPermission=true
// send permission requests and execute after approval.
func TestWriteToolSendsPermissionRequest(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode) // Agent mode enabled

	toolCalled := false
	writeTool := createTestWriteTool("test_write_tool", &toolCalled)
	metadata := createToolMetadata("test_write_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, chatModeRef, nil, metadata,
	)

	// Start tool invocation in goroutine since it will block waiting for permission
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = wrappedTools[0].Handler(copilot.ToolInvocation{
			ToolCallID: "test_write",
			ToolName:   "test_write_tool",
		})
	}()

	// Wait for permission request and approve it
	waitForPermissionRequestAndRespond(t, eventChan, "test_write_tool", true)

	// Wait for handler to complete
	<-done

	if !toolCalled {
		t.Error("Tool should have been called after permission approval")
	}
}

// TestWriteToolDeniedPermission verifies that tools are blocked when permission is denied.
func TestWriteToolDeniedPermission(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode) // Agent mode enabled

	toolCalled := false
	writeTool := createTestWriteTool("test_write_tool2", &toolCalled)
	metadata := createToolMetadata("test_write_tool2")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, chatModeRef, nil, metadata,
	)

	// Start tool invocation in goroutine
	var result copilot.ToolResult

	done := make(chan struct{})

	go func() {
		defer close(done)

		result, _ = wrappedTools[0].Handler(copilot.ToolInvocation{
			ToolCallID: "test_write_denied",
			ToolName:   "test_write_tool2",
		})
	}()

	// Wait for permission request and deny it
	waitForPermissionRequestAndRespond(t, eventChan, "test_write_tool2", false)

	// Wait for handler to complete
	<-done

	if toolCalled {
		t.Error("Tool should NOT have been called after permission denial")
	}

	if result.ResultType != "failure" {
		t.Errorf("Expected failure result, got %s", result.ResultType)
	}

	if !strings.Contains(result.TextResultForLLM, "Permission denied") {
		t.Errorf("Expected denial message, got: %s", result.TextResultForLLM)
	}
}

// TestReadToolSkipsPermission verifies that read-only tools execute without permission requests.
func TestReadToolSkipsPermission(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode) // Agent mode enabled

	toolCalled := false
	readTool := copilot.Tool{
		Name:        "test_read_tool",
		Description: "A read tool",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			toolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Read operation completed",
				ResultType:       successResult,
			}, nil
		},
	}

	// Metadata with RequiresPermission=false (read-only)
	metadata := map[string]toolgen.ToolDefinition{
		"test_read_tool": {
			Name:               "test_read_tool",
			RequiresPermission: false,
		},
	}

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{readTool}, eventChan, chatModeRef, nil, metadata,
	)

	// Execute directly - should not send permission request
	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_read",
		ToolName:   "test_read_tool",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check no permission request was sent
	select {
	case msg := <-eventChan:
		t.Errorf("Read tool should not send permission request, got: %T", msg)
	default:
		// Good - no message sent
	}

	if !toolCalled {
		t.Error("Read tool should have been called directly")
	}

	if result.ResultType != successResult {
		t.Errorf("Expected success, got %s", result.ResultType)
	}
}

// TestYoloModeAutoApproves verifies that YOLO mode auto-approves write tools
// without sending permission requests.
func TestYoloModeAutoApproves(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode)
	yoloModeRef := chatui.NewYoloModeRef(true) // YOLO mode enabled

	toolCalled := false
	writeTool := createTestWriteTool("test_yolo_tool", &toolCalled)
	metadata := createToolMetadata("test_yolo_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, chatModeRef, yoloModeRef, metadata,
	)

	// Execute directly - should auto-approve without permission prompt
	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_yolo",
		ToolName:   "test_yolo_tool",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Check no permission request was sent
	select {
	case msg := <-eventChan:
		t.Errorf("YOLO mode should not send permission request, got: %T", msg)
	default:
		// Good - no message sent
	}

	if !toolCalled {
		t.Error("Tool should have been called in YOLO mode")
	}

	if result.ResultType != successResult {
		t.Errorf("Expected success, got %s", result.ResultType)
	}
}

// TestYoloModeDisabledStillPrompts verifies that with YOLO mode disabled,
// write tools still require permission.
func TestYoloModeDisabledStillPrompts(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode)
	yoloModeRef := chatui.NewYoloModeRef(false) // YOLO mode disabled

	toolCalled := false
	writeTool := createTestWriteTool("test_yolo_off_tool", &toolCalled)
	metadata := createToolMetadata("test_yolo_off_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, chatModeRef, yoloModeRef, metadata,
	)

	// Start tool invocation in goroutine since it will block waiting for permission
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = wrappedTools[0].Handler(copilot.ToolInvocation{
			ToolCallID: "test_yolo_off",
			ToolName:   "test_yolo_off_tool",
		})
	}()

	// Wait for permission request and approve it
	waitForPermissionRequestAndRespond(t, eventChan, "test_yolo_off_tool", true)

	// Wait for handler to complete
	<-done

	if !toolCalled {
		t.Error("Tool should have been called after permission approval")
	}
}

// TestForceInjection verifies that the force flag is injected into tool arguments
// when a tool requiring permission is approved and its schema defines a force parameter.
func TestForceInjection(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode)
	yoloModeRef := chatui.NewYoloModeRef(true) // YOLO mode to avoid permission prompt

	var receivedArgs map[string]any

	writeTool := copilot.Tool{
		Name:        "test_force_tool",
		Description: "A write tool",
		Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
			args, isMap := inv.Arguments.(map[string]any)
			if isMap {
				receivedArgs = args
			}

			return copilot.ToolResult{
				TextResultForLLM: "Done",
				ResultType:       successResult,
			}, nil
		},
	}

	metadata := createToolMetadataWithForce("test_force_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, chatModeRef, yoloModeRef, metadata,
	)

	_, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_force",
		ToolName:   "test_force_tool",
		Arguments:  map[string]any{"name": "my-cluster"},
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if receivedArgs == nil {
		t.Fatal("Expected arguments to be passed to handler")
	}

	forceVal, forceExists := receivedArgs["force"]
	if !forceExists {
		t.Error("Expected 'force' argument to be injected")
	}

	forceBool, isBool := forceVal.(bool)
	if !isBool || !forceBool {
		t.Errorf("Expected force=true, got %v", forceVal)
	}

	// Verify original args are preserved
	nameVal, nameExists := receivedArgs["name"]
	if !nameExists || nameVal != "my-cluster" {
		t.Errorf("Expected original 'name' arg preserved, got %v", nameVal)
	}
}

// TestForceNotInjectedWithoutSchemaSupport verifies that the force flag is NOT injected
// when the tool's parameter schema does not define a force property.
func TestForceNotInjectedWithoutSchemaSupport(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode)
	yoloModeRef := chatui.NewYoloModeRef(true) // YOLO mode to avoid permission prompt

	var receivedArgs map[string]any

	writeTool := copilot.Tool{
		Name:        "test_no_force_tool",
		Description: "A write tool without force support",
		Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
			args, isMap := inv.Arguments.(map[string]any)
			if isMap {
				receivedArgs = args
			}

			return copilot.ToolResult{
				TextResultForLLM: "Done",
				ResultType:       successResult,
			}, nil
		},
	}

	// Use metadata WITHOUT force in schema
	metadata := createToolMetadata("test_no_force_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, chatModeRef, yoloModeRef, metadata,
	)

	_, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_no_force",
		ToolName:   "test_no_force_tool",
		Arguments:  map[string]any{"name": "my-cluster"},
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if receivedArgs == nil {
		t.Fatal("Expected arguments to be passed to handler")
	}

	// Verify force was NOT injected
	if _, forceExists := receivedArgs["force"]; forceExists {
		t.Error("Expected 'force' NOT to be injected for tool without force in schema")
	}

	// Verify original args are preserved
	nameVal, nameExists := receivedArgs["name"]
	if !nameExists || nameVal != "my-cluster" {
		t.Errorf("Expected original 'name' arg preserved, got %v", nameVal)
	}
}

// TestNilEventChanAutoApproves verifies that write tools with a nil event channel
// auto-approve instead of deadlocking (non-TUI mode).
func TestNilEventChanAutoApproves(t *testing.T) {
	t.Parallel()

	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode)

	toolCalled := false
	writeTool := createTestWriteTool("test_nil_chan_tool", &toolCalled)
	metadata := createToolMetadata("test_nil_chan_tool")

	// Pass nil for eventChan — simulates non-TUI mode
	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, nil, chatModeRef, nil, metadata,
	)

	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_nil_chan",
		ToolName:   "test_nil_chan_tool",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !toolCalled {
		t.Error("Tool should have been called with nil eventChan (auto-approve)")
	}

	if result.ResultType != successResult {
		t.Errorf("Expected success, got %s", result.ResultType)
	}
}

// TestAskModeBlocksWriteToolsAllowsReadTools verifies that ask mode blocks write tools
// (RequiresPermission=true) while allowing read-only tools to execute.
func TestAskModeBlocksWriteToolsAllowsReadTools(t *testing.T) {
	t.Parallel()

	metadata := map[string]toolgen.ToolDefinition{
		"test_ask_write": {
			Name:               "test_ask_write",
			RequiresPermission: true, // write tool
		},
		"test_ask_read": {
			Name:               "test_ask_read",
			RequiresPermission: false, // read-only tool
		},
	}

	t.Run("BlocksWriteTools", func(t *testing.T) {
		t.Parallel()
		assertAskModeBlocksWriteTool(t, metadata)
	})

	t.Run("AllowsReadTools", func(t *testing.T) {
		t.Parallel()
		assertAskModeAllowsReadTool(t, metadata)
	})
}

func assertAskModeBlocksWriteTool(t *testing.T, metadata map[string]toolgen.ToolDefinition) {
	t.Helper()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AskMode)

	writeToolCalled := false
	writeTool := copilot.Tool{
		Name:        "test_ask_write",
		Description: "A write tool",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			writeToolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Write executed",
				ResultType:       successResult,
			}, nil
		},
	}

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, chatModeRef, nil, metadata,
	)

	writeResult, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_ask_w",
		ToolName:   "test_ask_write",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if writeToolCalled {
		t.Error("Write tool should NOT be called in ask mode")
	}

	if writeResult.ResultType != failureResult {
		t.Errorf("Expected failure for write tool in ask mode, got %s", writeResult.ResultType)
	}

	if !strings.Contains(writeResult.TextResultForLLM, "Write tool blocked") {
		t.Errorf("Expected ask mode blocking message, got: %s", writeResult.TextResultForLLM)
	}
}

func assertAskModeAllowsReadTool(t *testing.T, metadata map[string]toolgen.ToolDefinition) {
	t.Helper()

	eventChan := make(chan tea.Msg, 10)
	chatModeRef := chatui.NewChatModeRef(chatui.AskMode)

	readToolCalled := false
	readTool := copilot.Tool{
		Name:        "test_ask_read",
		Description: "A read tool",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			readToolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Read executed",
				ResultType:       successResult,
			}, nil
		},
	}

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{readTool}, eventChan, chatModeRef, nil, metadata,
	)

	readResult, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_ask_r",
		ToolName:   "test_ask_read",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !readToolCalled {
		t.Error("Read tool should be called in ask mode")
	}

	if readResult.ResultType != successResult {
		t.Errorf("Expected success for read tool in ask mode, got %s", readResult.ResultType)
	}
}

// TestLoadChatConfig verifies that chat configuration is loaded correctly from ksail.yaml.
func TestLoadChatConfig(t *testing.T) {
	tests := []struct {
name                    string
yamlContent             string
expectModel             string
expectReasoningEffort   string
}{
{
name: "both model and reasoningEffort set",
yamlContent: `apiVersion: ksail.devantler.tech/v1alpha1
kind: Cluster
spec:
  chat:
    model: gpt-5
    reasoningEffort: high
`,
expectModel:           "gpt-5",
expectReasoningEffort: "high",
},
{
name: "only model set",
yamlContent: `apiVersion: ksail.devantler.tech/v1alpha1
kind: Cluster
spec:
  chat:
    model: claude-sonnet-4.5
`,
expectModel:           "claude-sonnet-4.5",
expectReasoningEffort: "",
},
{
name: "only reasoningEffort set",
yamlContent: `apiVersion: ksail.devantler.tech/v1alpha1
kind: Cluster
spec:
  chat:
    reasoningEffort: medium
`,
expectModel:           "",
expectReasoningEffort: "medium",
},
{
name: "empty chat spec",
yamlContent: `apiVersion: ksail.devantler.tech/v1alpha1
kind: Cluster
spec:
  chat: {}
`,
expectModel:           "",
expectReasoningEffort: "",
},
{
name: "no chat spec",
yamlContent: `apiVersion: ksail.devantler.tech/v1alpha1
kind: Cluster
spec: {}
`,
expectModel:           "",
expectReasoningEffort: "",
},
}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test
			tmpDir := t.TempDir()

// Save original working directory
origDir, err := os.Getwd()
if err != nil {
t.Fatalf("Failed to get current directory: %v", err)
}

// Change to temp directory
if err := os.Chdir(tmpDir); err != nil {
t.Fatalf("Failed to change directory: %v", err)
}

// Restore original directory after test
defer func() {
if err := os.Chdir(origDir); err != nil {
t.Errorf("Failed to restore directory: %v", err)
}
}()

// Write ksail.yaml
if err := os.WriteFile("ksail.yaml", []byte(tt.yamlContent), 0600); err != nil {
t.Fatalf("Failed to write config file: %v", err)
}

// Load config
cfg := chat.LoadChatConfig()

// Verify model
if cfg.Model != tt.expectModel {
t.Errorf("Expected model %q, got %q", tt.expectModel, cfg.Model)
}

// Verify reasoningEffort
if cfg.ReasoningEffort != tt.expectReasoningEffort {
t.Errorf("Expected reasoningEffort %q, got %q", tt.expectReasoningEffort, cfg.ReasoningEffort)
}
})
}
}

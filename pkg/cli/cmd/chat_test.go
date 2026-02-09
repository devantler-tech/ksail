package cmd_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
)

const (
	failureResult = "failure"
)

// planModeTestCase defines a test case for plan/agent mode tool execution.
type planModeTestCase struct {
	name              string
	agentModeEnabled  bool
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
				ResultType:       "success",
			}, nil
		},
	}
}

// TestPlanModeToolExecution verifies agent/plan mode tool execution behavior using table-driven tests.
func TestPlanModeToolExecution(t *testing.T) {
	t.Parallel()

	tests := []planModeTestCase{
		{
			name:             "AgentModeAllowsExecution",
			agentModeEnabled: true,
			expectToolCalled: true,
			expectResultType: "success",
		},
		{
			name:             "PlanModeBlocksExecution",
			agentModeEnabled: false,
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
			runPlanModeTestCase(t, testCase)
		})
	}
}

func runPlanModeTestCase(t *testing.T, testCase planModeTestCase) {
	t.Helper()

	eventChan := make(chan tea.Msg, 10)
	agentModeRef := chatui.NewAgentModeRef(testCase.agentModeEnabled)

	toolCalled := false
	testTool := createTestTool(&toolCalled)

	wrappedTools := cmd.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{testTool}, eventChan, agentModeRef, nil,
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

// TestPlanModeToggle verifies that toggling between agent and plan modes works correctly.
func TestPlanModeToggle(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	agentModeRef := chatui.NewAgentModeRef(false) // Start in plan mode

	toolCalled := false
	testTool := copilot.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			toolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Tool executed successfully",
				ResultType:       "success",
			}, nil
		},
	}

	wrappedTools := cmd.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{testTool}, eventChan, agentModeRef, nil,
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

	agentModeRef.SetEnabled(true)

	result, _ = wrappedTool.Handler(copilot.ToolInvocation{
		ToolCallID: "test2", ToolName: "test_tool",
	})

	if !toolCalled {
		t.Error("Tool should be called after toggling to agent mode")
	}

	if result.ResultType != "success" {
		t.Errorf("Expected success after toggle, got %s", result.ResultType)
	}
}

// TestPlanModeBlocksMutableTools verifies that edit tools are blocked in plan mode.
func TestPlanModeBlocksMutableTools(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	agentModeRef := chatui.NewAgentModeRef(false) // Start in plan mode

	mutableToolCalled := false
	mutableTool := copilot.Tool{
		Name:        "ksail_cluster_create",
		Description: "Create a cluster",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			mutableToolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Cluster created",
				ResultType:       "success",
			}, nil
		},
	}

	wrappedTools := cmd.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{mutableTool}, eventChan, agentModeRef, nil,
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
	requiresPermission bool,
) map[string]toolgen.ToolDefinition {
	return map[string]toolgen.ToolDefinition{
		toolName: {
			Name:               toolName,
			RequiresPermission: requiresPermission,
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
	agentModeRef := chatui.NewAgentModeRef(true) // Agent mode enabled

	toolCalled := false
	writeTool := createTestWriteTool("test_write_tool", &toolCalled)
	metadata := createToolMetadata("test_write_tool", true)

	wrappedTools := cmd.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, agentModeRef, metadata,
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
	agentModeRef := chatui.NewAgentModeRef(true) // Agent mode enabled

	toolCalled := false
	writeTool := createTestWriteTool("test_write_tool2", &toolCalled)
	metadata := createToolMetadata("test_write_tool2", true)

	wrappedTools := cmd.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, agentModeRef, metadata,
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
	agentModeRef := chatui.NewAgentModeRef(true) // Agent mode enabled

	toolCalled := false
	readTool := copilot.Tool{
		Name:        "test_read_tool",
		Description: "A read tool",
		Handler: func(_ copilot.ToolInvocation) (copilot.ToolResult, error) {
			toolCalled = true

			return copilot.ToolResult{
				TextResultForLLM: "Read operation completed",
				ResultType:       "success",
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

	wrappedTools := cmd.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{readTool}, eventChan, agentModeRef, metadata,
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

	if result.ResultType != "success" {
		t.Errorf("Expected success, got %s", result.ResultType)
	}
}

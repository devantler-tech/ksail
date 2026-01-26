package cmd_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
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

	wrappedTools := cmd.WrapToolsWithPermissionAndMode(
		[]copilot.Tool{testTool}, eventChan, agentModeRef,
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

	wrappedTools := cmd.WrapToolsWithPermissionAndMode(
		[]copilot.Tool{testTool}, eventChan, agentModeRef,
	)
	wrappedTool := wrappedTools[0]

	// Phase 1: Plan mode should block
	result, _ := wrappedTool.Handler(copilot.ToolInvocation{
		ToolCallID: "test1", ToolName: "test_tool",
	})

	if toolCalled {
		t.Error("Tool should not be called in plan mode")
	}

	if result.ResultType != "failure" {
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

// TestPlanModeBlocksMutableTools verifies that mutable tools are blocked in plan mode.
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

	wrappedTools := cmd.WrapToolsWithPermissionAndMode(
		[]copilot.Tool{mutableTool}, eventChan, agentModeRef,
	)

	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_mutable",
		ToolName:   "ksail_cluster_create",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if mutableToolCalled {
		t.Error("Mutable tool should not have been called in plan mode")
	}

	if result.ResultType != "failure" {
		t.Errorf("Expected failure for blocked mutable tool, got %s", result.ResultType)
	}

	if !strings.Contains(result.TextResultForLLM, "Tool execution blocked") {
		t.Error("Expected blocking message for mutable tool in plan mode")
	}
}

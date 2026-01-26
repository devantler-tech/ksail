package cmd

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestPlanModeBlocksToolExecution verifies that plan mode blocks all tool execution.
func TestPlanModeBlocksToolExecution(t *testing.T) {
	eventChan := make(chan tea.Msg, 10)
	agentModeRef := &chatui.AgentModeRef{Enabled: true}

	// Create a simple test tool
	testToolCalled := false
	testTool := copilot.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
			testToolCalled = true
			return copilot.ToolResult{
				TextResultForLLM: "Tool executed successfully",
				ResultType:       "success",
			}, nil
		},
	}

	// Wrap the tool with permission and mode enforcement
	wrappedTools := wrapToolsWithPermissionAndMode([]copilot.Tool{testTool}, eventChan, agentModeRef)

	if len(wrappedTools) != 1 {
		t.Fatalf("Expected 1 wrapped tool, got %d", len(wrappedTools))
	}

	wrappedTool := wrappedTools[0]

	// Test 1: Agent mode (enabled) - tool should execute
	t.Run("AgentModeAllowsExecution", func(t *testing.T) {
		testToolCalled = false
		agentModeRef.SetEnabled(true)

		result, err := wrappedTool.Handler(copilot.ToolInvocation{
			ToolCallID: "test1",
			ToolName:   "test_tool",
		})

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// In agent mode, tool is executed (testToolCalled would be true)
		// But since it's read-only, it goes straight through
		if result.ResultType != "success" {
			t.Errorf("Expected success, got %s", result.ResultType)
		}
	})

	// Test 2: Plan mode (disabled) - tool should be blocked
	t.Run("PlanModeBlocksExecution", func(t *testing.T) {
		testToolCalled = false
		agentModeRef.SetEnabled(false) // Switch to plan mode

		result, err := wrappedTool.Handler(copilot.ToolInvocation{
			ToolCallID: "test2",
			ToolName:   "test_tool",
		})

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		// Verify tool was NOT executed
		if testToolCalled {
			t.Error("Tool should not have been called in plan mode")
		}

		// Verify blocking message
		if result.ResultType != "failure" {
			t.Errorf("Expected failure result type, got %s", result.ResultType)
		}

		expectedMsg := "Tool execution blocked - currently in Plan mode"
		if !strings.Contains(result.TextResultForLLM, expectedMsg) {
			t.Errorf("Expected blocking message to contain '%s', got: %s",
				expectedMsg, result.TextResultForLLM)
		}

		// Verify helpful instruction
		if !strings.Contains(result.TextResultForLLM, "Switch to Agent mode") {
			t.Error("Expected message to include instruction to switch to Agent mode")
		}
	})

	// Test 3: Toggle back to agent mode
	t.Run("TogglingBackToAgentMode", func(t *testing.T) {
		testToolCalled = false
		agentModeRef.SetEnabled(true) // Switch back to agent mode

		result, err := wrappedTool.Handler(copilot.ToolInvocation{
			ToolCallID: "test3",
			ToolName:   "test_tool",
		})

		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		if result.ResultType != "success" {
			t.Errorf("Expected success after toggling back, got %s", result.ResultType)
		}
	})
}

// TestPlanModeBlocksMutableTools verifies that mutable tools are blocked in plan mode.
func TestPlanModeBlocksMutableTools(t *testing.T) {
	eventChan := make(chan tea.Msg, 10)
	agentModeRef := &chatui.AgentModeRef{Enabled: false} // Start in plan mode

	// Create a mutable test tool (name matches mutability pattern)
	mutableToolCalled := false
	mutableTool := copilot.Tool{
		Name:        "ksail_cluster_create",
		Description: "Create a cluster",
		Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
			mutableToolCalled = true
			return copilot.ToolResult{
				TextResultForLLM: "Cluster created",
				ResultType:       "success",
			}, nil
		},
	}

	wrappedTools := wrapToolsWithPermissionAndMode([]copilot.Tool{mutableTool}, eventChan, agentModeRef)
	wrappedTool := wrappedTools[0]

	// Test: Mutable tool blocked in plan mode
	result, err := wrappedTool.Handler(copilot.ToolInvocation{
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

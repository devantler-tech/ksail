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

// TestModeToolExecution verifies agent mode tool execution behavior.
func TestModeToolExecution(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)

	toolCalled := false
	testTool := createTestTool(&toolCalled)

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{testTool}, eventChan, nil, nil,
	)

	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test1",
		ToolName:   "test_tool",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !toolCalled {
		t.Error("Expected tool to be called in agent mode")
	}

	if result.ResultType != successResult {
		t.Errorf("Expected result type %s, got %s", successResult, result.ResultType)
	}
}

// TestModeToggle verifies that mode toggling cycles correctly between Agent and Plan.
func TestModeToggle(t *testing.T) {
	t.Parallel()

	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode)

	if chatModeRef.Mode() != chatui.AgentMode {
		t.Error("Expected initial mode to be AgentMode")
	}

	chatModeRef.SetMode(chatModeRef.Mode().Next())

	if chatModeRef.Mode() != chatui.PlanMode {
		t.Errorf("Expected PlanMode after first toggle, got %v", chatModeRef.Mode())
	}

	chatModeRef.SetMode(chatModeRef.Mode().Next())

	if chatModeRef.Mode() != chatui.AgentMode {
		t.Errorf("Expected AgentMode after second toggle, got %v", chatModeRef.Mode())
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

	toolCalled := false
	writeTool := createTestWriteTool("test_write_tool", &toolCalled)
	metadata := createToolMetadata("test_write_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, nil, metadata,
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

	toolCalled := false
	writeTool := createTestWriteTool("test_write_tool2", &toolCalled)
	metadata := createToolMetadata("test_write_tool2")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, nil, metadata,
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
		[]copilot.Tool{readTool}, eventChan, nil, metadata,
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
	yoloModeRef := chatui.NewYoloModeRef(true) // YOLO mode enabled

	toolCalled := false
	writeTool := createTestWriteTool("test_yolo_tool", &toolCalled)
	metadata := createToolMetadata("test_yolo_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, yoloModeRef, metadata,
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
	yoloModeRef := chatui.NewYoloModeRef(false) // YOLO mode disabled

	toolCalled := false
	writeTool := createTestWriteTool("test_yolo_off_tool", &toolCalled)
	metadata := createToolMetadata("test_yolo_off_tool")

	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, eventChan, yoloModeRef, metadata,
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
		[]copilot.Tool{writeTool}, eventChan, yoloModeRef, metadata,
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
		[]copilot.Tool{writeTool}, eventChan, yoloModeRef, metadata,
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

	toolCalled := false
	writeTool := createTestWriteTool("test_nil_chan_tool", &toolCalled)
	metadata := createToolMetadata("test_nil_chan_tool")

	// Pass nil for eventChan — simulates non-TUI mode
	wrappedTools := chat.WrapToolsWithPermissionAndModeMetadata(
		[]copilot.Tool{writeTool}, nil, nil, metadata,
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

//nolint:funlen // Test data structure
func getLoadChatConfigTests() []struct {
	name                  string
	yamlContent           string
	expectModel           string
	expectReasoningEffort string
} {
	return []struct {
		name                  string
		yamlContent           string
		expectModel           string
		expectReasoningEffort string
	}{
		{
			name: "both model and reasoningEffort set",
			yamlContent: `apiVersion: ksail.io/v1alpha1
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
			yamlContent: `apiVersion: ksail.io/v1alpha1
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
			yamlContent: `apiVersion: ksail.io/v1alpha1
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
			yamlContent: `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  chat: {}
`,
			expectModel:           "",
			expectReasoningEffort: "",
		},
		{
			name: "no chat spec",
			yamlContent: `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec: {}
`,
			expectModel:           "",
			expectReasoningEffort: "",
		},
	}
}

// TestLoadChatConfig verifies that chat configuration is loaded correctly from ksail.yaml.
//
//nolint:paralleltest // Cannot use t.Parallel() because subtests use t.Chdir()
func TestLoadChatConfig(t *testing.T) {
	for _, testCase := range getLoadChatConfigTests() {
		t.Run(testCase.name, func(t *testing.T) {
			runLoadChatConfigTest(
				t, testCase.yamlContent, testCase.expectModel, testCase.expectReasoningEffort,
			)
		})
	}
}

func runLoadChatConfigTest(t *testing.T, yamlContent, expectModel, expectReasoningEffort string) {
	t.Helper()

	// Create temp directory for test
	tmpDir := t.TempDir()

	// Change to temp directory (saves original and restores automatically)
	t.Chdir(tmpDir)

	// Write ksail.yaml
	err := os.WriteFile("ksail.yaml", []byte(yamlContent), 0o600)
	if err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Load config
	cfg := chat.GetLoadChatConfig()()

	// Verify model
	if cfg.Model != expectModel {
		t.Errorf("Expected model %q, got %q", expectModel, cfg.Model)
	}

	// Verify reasoningEffort
	if cfg.ReasoningEffort != expectReasoningEffort {
		t.Errorf("Expected reasoningEffort %q, got %q", expectReasoningEffort, cfg.ReasoningEffort)
	}
}

// TestResolveModel verifies model resolution from flags and config.
func TestResolveModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		flagValue   string
		configValue string
		expected    string
	}{
		{
			name:        "flag takes priority over config",
			flagValue:   "gpt-5",
			configValue: "claude-sonnet-4.5",
			expected:    "gpt-5",
		},
		{
			name:        "uses config when flag is empty",
			flagValue:   "",
			configValue: "claude-sonnet-4.5",
			expected:    "claude-sonnet-4.5",
		},
		{
			name:        "returns empty when both are empty",
			flagValue:   "",
			configValue: "",
			expected:    "",
		},
		{
			name:        "returns empty when config is auto",
			flagValue:   "",
			configValue: "auto",
			expected:    "",
		},
		{
			name:        "flag works when config is auto",
			flagValue:   "gpt-5",
			configValue: "auto",
			expected:    "gpt-5",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.GetResolveModel()(testCase.flagValue, testCase.configValue)
			if result != testCase.expected {
				t.Errorf("Expected %q, got %q", testCase.expected, result)
			}
		})
	}
}

// TestValidateReasoningEffort verifies reasoning effort validation.
func TestValidateReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		effort    string
		expectErr bool
	}{
		{
			name:      "valid low",
			effort:    "low",
			expectErr: false,
		},
		{
			name:      "valid medium",
			effort:    "medium",
			expectErr: false,
		},
		{
			name:      "valid high",
			effort:    "high",
			expectErr: false,
		},
		{
			name:      "empty string is valid",
			effort:    "",
			expectErr: false,
		},
		{
			name:      "invalid value",
			effort:    "extreme",
			expectErr: true,
		},
		{
			name:      "case sensitive - Low should fail",
			effort:    "Low",
			expectErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := chat.GetValidateReasoningEffort()(testCase.effort)
			if testCase.expectErr && err == nil {
				t.Errorf("Expected error for effort %q, got nil", testCase.effort)
			}

			if !testCase.expectErr && err != nil {
				t.Errorf("Expected no error for effort %q, got %v", testCase.effort, err)
			}
		})
	}
}

// TestResolveReasoningEffort verifies reasoning effort resolution from flags and config.
func TestResolveReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		flagValue   string
		configValue string
		expected    string
		expectErr   bool
	}{
		{
			name:        "flag takes priority over config",
			flagValue:   "high",
			configValue: "medium",
			expected:    "high",
			expectErr:   false,
		},
		{
			name:        "uses valid config when flag is empty",
			flagValue:   "",
			configValue: "low",
			expected:    "low",
			expectErr:   false,
		},
		{
			name:        "returns empty when both are empty",
			flagValue:   "",
			configValue: "",
			expected:    "",
			expectErr:   false,
		},
		{
			name:        "flag ignores invalid config",
			flagValue:   "medium",
			configValue: "invalid",
			expected:    "medium",
			expectErr:   false,
		},
		{
			name:        "error on invalid config when flag is empty",
			flagValue:   "",
			configValue: "invalid",
			expected:    "",
			expectErr:   true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertResolveReasoningEffort(
				t,
				testCase.flagValue,
				testCase.configValue,
				testCase.expected,
				testCase.expectErr,
			)
		})
	}
}

func assertResolveReasoningEffort(
	t *testing.T,
	flagValue, configValue, expected string,
	expectErr bool,
) {
	t.Helper()

	result, err := chat.GetResolveReasoningEffort()(flagValue, configValue)

	if expectErr && err == nil {
		t.Errorf("Expected error, got nil")
	}

	if !expectErr && err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// TestFilterEnvVars verifies environment variable filtering.
func TestFilterEnvVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		environ    []string
		filterList []string
		expected   []string
	}{
		{
			name:       "filters single variable",
			environ:    []string{"PATH=/bin", "GITHUB_TOKEN=secret", "HOME=/home"},
			filterList: []string{"GITHUB_TOKEN"},
			expected:   []string{"PATH=/bin", "HOME=/home"},
		},
		{
			name: "filters multiple variables",
			environ: []string{
				"PATH=/bin",
				"GITHUB_TOKEN=secret",
				"GH_TOKEN=secret2",
				"HOME=/home",
			},
			filterList: []string{"GITHUB_TOKEN", "GH_TOKEN"},
			expected:   []string{"PATH=/bin", "HOME=/home"},
		},
		{
			name:       "no filtering when list is empty",
			environ:    []string{"PATH=/bin", "HOME=/home"},
			filterList: []string{},
			expected:   []string{"PATH=/bin", "HOME=/home"},
		},
		{
			name:       "preserves order",
			environ:    []string{"A=1", "B=2", "C=3", "D=4"},
			filterList: []string{"B"},
			expected:   []string{"A=1", "C=3", "D=4"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.GetFilterEnvVars()(testCase.environ, testCase.filterList)
			if len(result) != len(testCase.expected) {
				t.Fatalf("Expected %d vars, got %d", len(testCase.expected), len(result))
			}

			for i, expected := range testCase.expected {
				if result[i] != expected {
					t.Errorf("Position %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

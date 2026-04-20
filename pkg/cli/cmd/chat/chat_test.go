// Package chat_test provides unit tests for the chat command package.
package chat_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/chat"
	chatui "github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
)

const (
	successResult = "success"
	failureResult = "failure"
)

var (
	errAuthFetchFailed       = errors.New("auth check: fetch failed")
	errFetchFailed           = errors.New("fetch failed")
	errUnauthorized          = errors.New("unauthorized: authentication required")
	errNoResponsesConfigured = errors.New("mockAuthChecker: no responses configured")
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

	toolCalled := false
	testTool := createTestTool(&toolCalled)

	wrappedTools := chat.WrapToolsWithForceInjection(
		[]copilot.Tool{testTool}, nil,
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

// TestModeToggle verifies that mode toggling cycles correctly: Interactive → Plan → Autopilot → Interactive.
func TestModeToggle(t *testing.T) {
	t.Parallel()

	chatModeRef := chatui.NewChatModeRef(chatui.InteractiveMode)

	if chatModeRef.Mode() != chatui.InteractiveMode {
		t.Error("Expected initial mode to be InteractiveMode")
	}

	chatModeRef.SetMode(chatModeRef.Mode().Next())

	if chatModeRef.Mode() != chatui.PlanMode {
		t.Errorf("Expected PlanMode after first toggle, got %v", chatModeRef.Mode())
	}

	chatModeRef.SetMode(chatModeRef.Mode().Next())

	if chatModeRef.Mode() != chatui.AutopilotMode {
		t.Errorf("Expected AutopilotMode after second toggle, got %v", chatModeRef.Mode())
	}

	chatModeRef.SetMode(chatModeRef.Mode().Next())

	if chatModeRef.Mode() != chatui.InteractiveMode {
		t.Errorf("Expected InteractiveMode after third toggle, got %v", chatModeRef.Mode())
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

// TestForceInjection verifies that the force flag is injected into tool arguments
// when a tool's schema defines a force parameter.
func TestForceInjection(t *testing.T) {
	t.Parallel()

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

	wrappedTools := chat.WrapToolsWithForceInjection(
		[]copilot.Tool{writeTool}, metadata,
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

	wrappedTools := chat.WrapToolsWithForceInjection(
		[]copilot.Tool{writeTool}, metadata,
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

// TestToolExecutesDirectlyWithoutWrapper verifies that tools without metadata
// still execute correctly through the force injection wrapper.
func TestToolExecutesDirectlyWithoutWrapper(t *testing.T) {
	t.Parallel()

	toolCalled := false
	writeTool := createTestWriteTool("test_direct_tool", &toolCalled)

	// Pass nil metadata — tool should still execute
	wrappedTools := chat.WrapToolsWithForceInjection(
		[]copilot.Tool{writeTool}, nil,
	)

	result, err := wrappedTools[0].Handler(copilot.ToolInvocation{
		ToolCallID: "test_direct",
		ToolName:   "test_direct_tool",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !toolCalled {
		t.Error("Tool should have been called directly")
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

	filter := chat.GetFilterEnvVars()

	for _, testCase := range []struct {
		name       string
		environ    []string
		filterList []string
		expected   []string
	}{
		{
			name:       "filters matching vars",
			environ:    []string{"PATH=/bin", "GITHUB_TOKEN=s", "GH_TOKEN=s2", "HOME=/h"},
			filterList: []string{"GITHUB_TOKEN", "GH_TOKEN"},
			expected:   []string{"PATH=/bin", "HOME=/h"},
		},
		{
			name:       "empty filter list preserves all",
			environ:    []string{"PATH=/bin", "HOME=/h"},
			filterList: []string{},
			expected:   []string{"PATH=/bin", "HOME=/h"},
		},
		{
			name:       "preserves input order",
			environ:    []string{"A=1", "B=2", "C=3", "D=4"},
			filterList: []string{"C"},
			expected:   []string{"A=1", "B=2", "D=4"},
		},
		{
			name:       "non-matching filter keys are harmless",
			environ:    []string{"PATH=/bin", "HOME=/h"},
			filterList: []string{"NONEXISTENT"},
			expected:   []string{"PATH=/bin", "HOME=/h"},
		},
		{
			name:       "COPILOT_GITHUB_TOKEN filtered, user vars preserved",
			environ:    []string{"PATH=/bin", "COPILOT_GITHUB_TOKEN=t", "COPILOT_CUSTOM_INSTRUCTIONS_DIRS=/d"},
			filterList: []string{"COPILOT_GITHUB_TOKEN"},
			expected:   []string{"PATH=/bin", "COPILOT_CUSTOM_INSTRUCTIONS_DIRS=/d"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := filter(testCase.environ, testCase.filterList)
			if len(got) != len(testCase.expected) {
				t.Fatalf("expected %d vars, got %d: %v", len(testCase.expected), len(got), got)
			}

			for i := range testCase.expected {
				if got[i] != testCase.expected[i] {
					t.Errorf("pos %d: expected %q, got %q", i, testCase.expected[i], got[i])
				}
			}
		})
	}
}

// mockAuthChecker is a test double for chat.AuthStatusChecker that tracks
// call count and returns a configurable sequence of responses.
type mockAuthChecker struct {
	// responses to return in order; if calls exceed len(responses), the last is reused.
	responses []mockAuthResponse
	// callCount tracks the number of GetAuthStatus calls made.
	callCount atomic.Int32
}

type mockAuthResponse struct {
	status *copilot.GetAuthStatusResponse
	err    error
}

func (m *mockAuthChecker) GetAuthStatus(_ context.Context) (*copilot.GetAuthStatusResponse, error) {
	if len(m.responses) == 0 {
		return nil, errNoResponsesConfigured
	}

	idx := int(m.callCount.Add(1)) - 1
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}

	return m.responses[idx].status, m.responses[idx].err
}

// TestGetAuthStatusWithRetrySucceedsFirstAttempt verifies that no retries occur when
// GetAuthStatus succeeds on the first call.
func TestGetAuthStatusWithRetrySucceedsFirstAttempt(t *testing.T) {
	t.Parallel()

	login := "testuser"
	mock := &mockAuthChecker{
		responses: []mockAuthResponse{
			{
				status: &copilot.GetAuthStatusResponse{IsAuthenticated: true, Login: &login},
				err:    nil,
			},
		},
	}

	status, err := chat.GetAuthStatusWithRetry(context.Background(), mock)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if status == nil || !status.IsAuthenticated {
		t.Fatal("Expected authenticated status")
	}

	if mock.callCount.Load() != 1 {
		t.Errorf("Expected exactly 1 call, got %d", mock.callCount.Load())
	}
}

// TestGetAuthStatusWithRetryRetriesOnRetryableError verifies that a transient
// "fetch failed" error is retried and eventual success is returned.
func TestGetAuthStatusWithRetryRetriesOnRetryableError(t *testing.T) {
	t.Parallel()

	login := "testuser"
	mock := &mockAuthChecker{
		responses: []mockAuthResponse{
			{status: nil, err: errAuthFetchFailed},
			{
				status: &copilot.GetAuthStatusResponse{IsAuthenticated: true, Login: &login},
				err:    nil,
			},
		},
	}

	const (
		tinyBase = 1 * time.Millisecond
		tinyMax  = 2 * time.Millisecond
	)

	status, err := chat.GetAuthStatusWithRetryOpts(context.Background(), mock, tinyBase, tinyMax)
	if err != nil {
		t.Fatalf("Expected no error after retry, got: %v", err)
	}

	if status == nil || !status.IsAuthenticated {
		t.Fatal("Expected authenticated status on retry success")
	}

	if mock.callCount.Load() < 2 {
		t.Errorf("Expected at least 2 calls (retry), got %d", mock.callCount.Load())
	}
}

// TestGetAuthStatusWithRetryStopsOnNonRetryableError verifies that a permanent
// error (e.g., unauthorized) is not retried and the error is returned immediately,
// wrapped with attempt context.
func TestGetAuthStatusWithRetryStopsOnNonRetryableError(t *testing.T) {
	t.Parallel()

	mock := &mockAuthChecker{
		responses: []mockAuthResponse{
			{status: nil, err: errUnauthorized},
		},
	}

	_, err := chat.GetAuthStatusWithRetry(context.Background(), mock)
	if err == nil {
		t.Fatal("Expected error for non-retryable failure")
	}

	if !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("Expected unauthorized error, got: %v", err)
	}

	if !strings.Contains(err.Error(), "non-retryable") {
		t.Errorf("Expected error to mention non-retryable, got: %v", err)
	}

	if mock.callCount.Load() != 1 {
		t.Errorf(
			"Expected exactly 1 call (no retry on non-retryable), got %d",
			mock.callCount.Load(),
		)
	}
}

// TestGetAuthStatusWithRetryExhaustedRetries verifies that when all retries are
// exhausted the returned error is wrapped with attempt count and max retries context.
func TestGetAuthStatusWithRetryExhaustedRetries(t *testing.T) {
	t.Parallel()

	// Build authMaxAttempts responses, all retryable ("fetch failed").
	responses := make([]mockAuthResponse, chat.AuthMaxAttempts)
	for i := range responses {
		responses[i] = mockAuthResponse{status: nil, err: errAuthFetchFailed}
	}

	mock := &mockAuthChecker{responses: responses}

	const (
		tinyBase = 1 * time.Millisecond
		tinyMax  = 2 * time.Millisecond
	)

	_, err := chat.GetAuthStatusWithRetryOpts(context.Background(), mock, tinyBase, tinyMax)
	if err == nil {
		t.Fatal("Expected error after exhausting retries")
	}

	if !strings.Contains(err.Error(), "fetch failed") {
		t.Errorf("Expected original error preserved, got: %v", err)
	}

	expectedMsg := fmt.Sprintf(
		"auth status check failed after %d/%d attempts",
		chat.AuthMaxAttempts,
		chat.AuthMaxAttempts,
	)
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain %q, got: %v", expectedMsg, err)
	}

	if int(mock.callCount.Load()) != chat.AuthMaxAttempts {
		t.Errorf("Expected exactly %d calls, got %d", chat.AuthMaxAttempts, mock.callCount.Load())
	}
}

// TestGetAuthStatusWithRetryContextCancellation verifies that in-flight retry
// waits respect context cancellation and return the cancellation error promptly.
func TestGetAuthStatusWithRetryContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context immediately after the first call returns.
	var called atomic.Bool

	mock := &mockAuthChecker{
		responses: []mockAuthResponse{
			{status: nil, err: errFetchFailed},
		},
	}

	// Override: cancel ctx as soon as first GetAuthStatus returns.
	cancellingMock := &cancelOnCallMock{inner: mock, cancel: cancel, called: &called}

	// Use tiny backoff durations so the timer fires well before any CI timeout,
	// and cancellation can be detected quickly without relying on wall-clock thresholds.
	const (
		tinyBase = 5 * time.Millisecond
		tinyMax  = 10 * time.Millisecond
	)

	start := time.Now()
	_, err := chat.GetAuthStatusWithRetryOpts(ctx, cancellingMock, tinyBase, tinyMax)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected error due to context cancellation")
	}

	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("Expected cancellation error, got: %v", err)
	}

	// With tiny backoff durations the whole call should complete in well under 1s.
	if elapsed > time.Second {
		t.Errorf("Expected fast cancellation, took %v", elapsed)
	}
}

// cancelOnCallMock wraps a mock and cancels the context after the first call.
type cancelOnCallMock struct {
	inner  chat.AuthStatusChecker
	cancel context.CancelFunc
	called *atomic.Bool
}

func (m *cancelOnCallMock) GetAuthStatus(
	ctx context.Context,
) (*copilot.GetAuthStatusResponse, error) {
	resp, err := m.inner.GetAuthStatus(ctx)
	// Cancel after the first call so the retry wait is interrupted.
	if m.called.CompareAndSwap(false, true) {
		m.cancel()
	}

	return resp, err //nolint:wrapcheck // Mock function, wrapping not needed
}

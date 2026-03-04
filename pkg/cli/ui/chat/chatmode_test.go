package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	"github.com/github/copilot-sdk/go/rpc"
)

// TestChatMode_String tests the String method for all chat modes.
func TestChatMode_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     chat.ChatMode
		expected string
	}{
		{name: "agent mode string", mode: chat.AgentMode, expected: "agent"},
		{name: "plan mode string", mode: chat.PlanMode, expected: "plan"},
		{name: "unknown mode defaults to agent", mode: chat.ChatMode(99), expected: "agent"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.mode.String(); got != tc.expected {
				t.Errorf("ChatMode(%d).String() = %q, want %q", tc.mode, got, tc.expected)
			}
		})
	}
}

// TestChatMode_Icon tests the Icon method for all chat modes.
func TestChatMode_Icon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     chat.ChatMode
		expected string
	}{
		{name: "agent mode icon", mode: chat.AgentMode, expected: "</>"},
		{name: "plan mode icon", mode: chat.PlanMode, expected: "\u2261"},
		{name: "unknown mode defaults to agent icon", mode: chat.ChatMode(99), expected: "</>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.mode.Icon(); got != tc.expected {
				t.Errorf("ChatMode(%d).Icon() = %q, want %q", tc.mode, got, tc.expected)
			}
		})
	}
}

// TestChatMode_Label tests the Label method for all chat modes.
func TestChatMode_Label(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     chat.ChatMode
		expected string
	}{
		{name: "agent mode label", mode: chat.AgentMode, expected: "</> agent"},
		{name: "plan mode label", mode: chat.PlanMode, expected: "\u2261 plan"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.mode.Label(); got != tc.expected {
				t.Errorf("ChatMode(%d).Label() = %q, want %q", tc.mode, got, tc.expected)
			}
		})
	}
}

// TestChatMode_Next tests the Next method cycles correctly: Agent → Plan → Agent.
func TestChatMode_Next(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     chat.ChatMode
		expected chat.ChatMode
	}{
		{name: "agent cycles to plan", mode: chat.AgentMode, expected: chat.PlanMode},
		{name: "plan cycles to agent", mode: chat.PlanMode, expected: chat.AgentMode},
		{name: "unknown defaults to agent", mode: chat.ChatMode(99), expected: chat.AgentMode},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.mode.Next(); got != tc.expected {
				t.Errorf("ChatMode(%d).Next() = %v, want %v", tc.mode, got, tc.expected)
			}
		})
	}
}

// TestChatMode_NextFullCycle verifies a complete Agent → Plan → Agent cycle.
func TestChatMode_NextFullCycle(t *testing.T) {
	t.Parallel()

	mode := chat.AgentMode

	// Agent → Plan
	mode = mode.Next()
	if mode != chat.PlanMode {
		t.Fatalf("Expected PlanMode after first Next(), got %v", mode)
	}

	// Plan → Agent
	mode = mode.Next()
	if mode != chat.AgentMode {
		t.Fatalf("Expected AgentMode after second Next(), got %v", mode)
	}
}

// TestChatMode_ToSDKMode tests mapping to SDK RPC modes.
func TestChatMode_ToSDKMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     chat.ChatMode
		expected rpc.Mode
	}{
		{name: "agent maps to interactive", mode: chat.AgentMode, expected: rpc.Interactive},
		{name: "plan maps to plan", mode: chat.PlanMode, expected: rpc.Plan},
		{name: "unknown defaults to interactive", mode: chat.ChatMode(99), expected: rpc.Interactive},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.mode.ToSDKMode(); got != tc.expected {
				t.Errorf("ChatMode(%d).ToSDKMode() = %v, want %v", tc.mode, got, tc.expected)
			}
		})
	}
}

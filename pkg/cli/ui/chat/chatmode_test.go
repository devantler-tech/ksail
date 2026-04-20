package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
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
		{name: "interactive mode string", mode: chat.InteractiveMode, expected: "interactive"},
		{name: "plan mode string", mode: chat.PlanMode, expected: "plan"},
		{name: "autopilot mode string", mode: chat.AutopilotMode, expected: "autopilot"},
		{
			name:     "unknown mode defaults to interactive",
			mode:     chat.ChatMode(99),
			expected: "interactive",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.String(); got != testCase.expected {
				t.Errorf(
					"ChatMode(%d).String() = %q, want %q",
					testCase.mode,
					got,
					testCase.expected,
				)
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
		{name: "interactive mode icon", mode: chat.InteractiveMode, expected: "</>"},
		{name: "plan mode icon", mode: chat.PlanMode, expected: "≡"},
		{name: "autopilot mode icon", mode: chat.AutopilotMode, expected: "⚡"},
		{
			name:     "unknown mode defaults to interactive icon",
			mode:     chat.ChatMode(99),
			expected: "</>",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.Icon(); got != testCase.expected {
				t.Errorf("ChatMode(%d).Icon() = %q, want %q", testCase.mode, got, testCase.expected)
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
		{name: "interactive mode label", mode: chat.InteractiveMode, expected: "</> interactive"},
		{name: "plan mode label", mode: chat.PlanMode, expected: "≡ plan"},
		{name: "autopilot mode label", mode: chat.AutopilotMode, expected: "⚡ autopilot"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.Label(); got != testCase.expected {
				t.Errorf(
					"ChatMode(%d).Label() = %q, want %q",
					testCase.mode,
					got,
					testCase.expected,
				)
			}
		})
	}
}

// TestChatMode_Next tests the Next method cycles correctly through all modes.
func TestChatMode_Next(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     chat.ChatMode
		expected chat.ChatMode
	}{
		{name: "interactive cycles to plan", mode: chat.InteractiveMode, expected: chat.PlanMode},
		{name: "plan cycles to autopilot", mode: chat.PlanMode, expected: chat.AutopilotMode},
		{
			name:     "autopilot cycles to interactive",
			mode:     chat.AutopilotMode,
			expected: chat.InteractiveMode,
		},
		{
			name:     "unknown defaults to interactive",
			mode:     chat.ChatMode(99),
			expected: chat.InteractiveMode,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.Next(); got != testCase.expected {
				t.Errorf("ChatMode(%d).Next() = %v, want %v", testCase.mode, got, testCase.expected)
			}
		})
	}
}

// TestChatMode_NextFullCycle verifies a complete cycle through all three modes.
func TestChatMode_NextFullCycle(t *testing.T) {
	t.Parallel()

	mode := chat.InteractiveMode

	mode = mode.Next()
	if mode != chat.PlanMode {
		t.Fatalf("Expected PlanMode after first Next(), got %v", mode)
	}

	mode = mode.Next()
	if mode != chat.AutopilotMode {
		t.Fatalf("Expected AutopilotMode after second Next(), got %v", mode)
	}

	mode = mode.Next()
	if mode != chat.InteractiveMode {
		t.Fatalf("Expected InteractiveMode after third Next(), got %v", mode)
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
		{
			name:     "interactive maps to interactive",
			mode:     chat.InteractiveMode,
			expected: rpc.ModeInteractive,
		},
		{name: "plan maps to plan", mode: chat.PlanMode, expected: rpc.ModePlan},
		{
			name:     "autopilot maps to autopilot",
			mode:     chat.AutopilotMode,
			expected: rpc.ModeAutopilot,
		},
		{
			name:     "unknown defaults to interactive",
			mode:     chat.ChatMode(99),
			expected: rpc.ModeInteractive,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.mode.ToSDKMode(); got != testCase.expected {
				t.Errorf(
					"ChatMode(%d).ToSDKMode() = %v, want %v",
					testCase.mode,
					got,
					testCase.expected,
				)
			}
		})
	}
}

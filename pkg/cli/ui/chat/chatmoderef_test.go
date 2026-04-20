package chat_test

import (
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
)

// TestChatModeRef tests the thread-safe chat mode reference.
func TestChatModeRef(t *testing.T) {
	t.Parallel()

	ref := chat.NewChatModeRef(chat.InteractiveMode)

	// Test initial state
	if ref.Mode() != chat.InteractiveMode {
		t.Errorf("Expected interactive mode initially, got %v", ref.Mode())
	}

	// Test setting to plan mode
	ref.SetMode(chat.PlanMode)

	if ref.Mode() != chat.PlanMode {
		t.Errorf("Expected plan mode after SetMode(PlanMode), got %v", ref.Mode())
	}

	// Test setting to autopilot mode
	ref.SetMode(chat.AutopilotMode)

	if ref.Mode() != chat.AutopilotMode {
		t.Errorf("Expected autopilot mode after SetMode(AutopilotMode), got %v", ref.Mode())
	}

	// Test setting back to interactive mode
	ref.SetMode(chat.InteractiveMode)

	if ref.Mode() != chat.InteractiveMode {
		t.Errorf("Expected interactive mode after SetMode(InteractiveMode), got %v", ref.Mode())
	}
}

// TestChatModeRefConcurrency tests concurrent access to chat mode reference.
func TestChatModeRefConcurrency(t *testing.T) {
	t.Parallel()

	ref := chat.NewChatModeRef(chat.InteractiveMode)

	modes := []chat.ChatMode{chat.InteractiveMode, chat.PlanMode, chat.AutopilotMode}

	var waitGroup sync.WaitGroup

	// Start multiple goroutines that cycle through modes
	for idx := range 100 {
		waitGroup.Add(1)

		go func(modeIdx int) {
			defer waitGroup.Done()

			ref.SetMode(modes[modeIdx%len(modes)])
			_ = ref.Mode()
		}(idx)
	}

	waitGroup.Wait()
	// Test passes if no race conditions occur
}

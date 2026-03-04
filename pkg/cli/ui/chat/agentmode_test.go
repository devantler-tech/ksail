package chat_test

import (
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
)

// TestChatModeRef tests the thread-safe chat mode reference.
func TestChatModeRef(t *testing.T) {
	t.Parallel()

	ref := chat.NewChatModeRef(chat.AgentMode)

	// Test initial state
	if ref.Mode() != chat.AgentMode {
		t.Errorf("Expected agent mode initially, got %v", ref.Mode())
	}

	// Test setting to plan mode
	ref.SetMode(chat.PlanMode)

	if ref.Mode() != chat.PlanMode {
		t.Errorf("Expected plan mode after SetMode(PlanMode), got %v", ref.Mode())
	}

	// Test setting back to agent mode
	ref.SetMode(chat.AgentMode)

	if ref.Mode() != chat.AgentMode {
		t.Errorf("Expected agent mode after SetMode(AgentMode), got %v", ref.Mode())
	}
}

// TestChatModeRefConcurrency tests concurrent access to chat mode reference.
func TestChatModeRefConcurrency(t *testing.T) {
	t.Parallel()

	ref := chat.NewChatModeRef(chat.AgentMode)

	modes := []chat.ChatMode{chat.AgentMode, chat.PlanMode}

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

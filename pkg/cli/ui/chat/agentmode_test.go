package chat_test

import (
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
)

// TestAgentModeRef tests the thread-safe agent mode reference.
func TestAgentModeRef(t *testing.T) {
	t.Parallel()

	ref := chat.NewAgentModeRef(true)

	// Test initial state
	if !ref.IsEnabled() {
		t.Error("Expected agent mode to be enabled initially")
	}

	// Test setting to false
	ref.SetEnabled(false)

	if ref.IsEnabled() {
		t.Error("Expected agent mode to be disabled after SetEnabled(false)")
	}

	// Test setting back to true
	ref.SetEnabled(true)

	if !ref.IsEnabled() {
		t.Error("Expected agent mode to be enabled after SetEnabled(true)")
	}
}

// TestAgentModeRefConcurrency tests concurrent access to agent mode reference.
func TestAgentModeRefConcurrency(t *testing.T) {
	t.Parallel()

	ref := chat.NewAgentModeRef(true)

	var waitGroup sync.WaitGroup

	// Start multiple goroutines that toggle the mode
	for idx := range 100 {
		waitGroup.Add(1)

		go func(enabled bool) {
			defer waitGroup.Done()

			ref.SetEnabled(enabled)
			_ = ref.IsEnabled()
		}(idx%2 == 0)
	}

	waitGroup.Wait()
	// Test passes if no race conditions occur
}

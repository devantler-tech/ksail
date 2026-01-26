package chat

import (
	"sync"
	"testing"
)

// TestAgentModeRef tests the thread-safe agent mode reference.
func TestAgentModeRef(t *testing.T) {
	ref := &AgentModeRef{Enabled: true}

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
	ref := &AgentModeRef{Enabled: true}
	var wg sync.WaitGroup

	// Start multiple goroutines that toggle the mode
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(enabled bool) {
			defer wg.Done()
			ref.SetEnabled(enabled)
			_ = ref.IsEnabled()
		}(i%2 == 0)
	}

	wg.Wait()
	// Test passes if no race conditions occur
}

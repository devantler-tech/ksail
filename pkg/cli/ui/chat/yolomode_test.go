package chat_test

import (
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
)

// TestYoloModeRef tests the thread-safe YOLO mode reference.
func TestYoloModeRef(t *testing.T) {
	t.Parallel()

	ref := chat.NewYoloModeRef(false)

	// Test initial state (disabled by default)
	if ref.IsEnabled() {
		t.Error("Expected YOLO mode to be disabled initially")
	}

	// Test enabling
	ref.SetEnabled(true)

	if !ref.IsEnabled() {
		t.Error("Expected YOLO mode to be enabled after SetEnabled(true)")
	}

	// Test disabling
	ref.SetEnabled(false)

	if ref.IsEnabled() {
		t.Error("Expected YOLO mode to be disabled after SetEnabled(false)")
	}
}

// TestYoloModeRefConcurrency tests concurrent access to YOLO mode reference.
func TestYoloModeRefConcurrency(t *testing.T) {
	t.Parallel()

	ref := chat.NewYoloModeRef(false)

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

package workload

import (
	"sync"
	"time"
)

// debounceInterval is the time to wait after the last file event before
// triggering an apply. This prevents redundant reconciles during batch saves.
const debounceInterval = 500 * time.Millisecond

// debounceState holds the mutable state shared between the event loop and
// debounce timer callbacks.
type debounceState struct {
	timer      *time.Timer
	mutex      sync.Mutex
	lastFile   string
	generation uint64
}

// cancelPendingDebounce increments the generation counter to invalidate any
// pending timer callback and stops the timer if active.
func cancelPendingDebounce(state *debounceState) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.generation++

	if state.timer != nil {
		state.timer.Stop()
	}
}

// scheduleApply updates the debounce state and (re)starts the timer.
func scheduleApply(state *debounceState, file string, applyCh chan string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.lastFile = file
	state.generation++

	currentGen := state.generation

	if state.timer != nil {
		state.timer.Stop()
	}

	state.timer = time.AfterFunc(debounceInterval, func() {
		enqueueIfCurrent(state, currentGen, applyCh)
	})
}

// enqueueIfCurrent checks whether the generation is still current and, if so,
// coalesces any stale pending apply and enqueues the latest file.
func enqueueIfCurrent(state *debounceState, expectedGen uint64, applyCh chan string) {
	state.mutex.Lock()

	if expectedGen != state.generation {
		state.mutex.Unlock()

		return
	}

	file := state.lastFile
	state.mutex.Unlock()

	// Coalesce: drain any stale pending apply, then enqueue latest.
	// NOTE: safe because the generation guard above ensures only one
	// timer callback is active at any time (single sender).
	select {
	case <-applyCh:
	default:
	}

	select {
	case applyCh <- file:
	default:
	}
}

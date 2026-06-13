// Package workloadwatch provides the cobra-free engine behind the
// `ksail workload watch` command: a generation-counted debounce timer, fsnotify
// directory registration/event filtering, and a modification-time polling
// fallback for environments where inotify drops events. Apply and Flux-reconcile
// side effects stay in the command layer and are driven by the file paths this
// engine enqueues on a shared apply channel.
package workloadwatch

import (
	"sync"
	"time"
)

// DebounceInterval is the time to wait after the last file event before
// triggering an apply. This prevents redundant reconciles during batch saves.
const DebounceInterval = 500 * time.Millisecond

// DebounceState holds the mutable state shared between the event loop and
// debounce timer callbacks.
type DebounceState struct {
	timer      *time.Timer
	mutex      sync.Mutex
	lastFile   string
	generation uint64
}

// Generation returns the current generation counter under the state mutex.
func (state *DebounceState) Generation() uint64 {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	return state.generation
}

// LastFile returns the most recently scheduled file under the state mutex.
func (state *DebounceState) LastFile() string {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	return state.lastFile
}

// Set assigns the generation and lastFile fields under the state mutex. It is a
// test seam for driving the debounce machinery deterministically.
func (state *DebounceState) Set(generation uint64, lastFile string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.generation = generation
	state.lastFile = lastFile
}

// CancelPendingDebounce increments the generation counter to invalidate any
// pending timer callback and stops the timer if active.
func CancelPendingDebounce(state *DebounceState) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.generation++

	if state.timer != nil {
		state.timer.Stop()
	}
}

// ScheduleApply updates the debounce state and (re)starts the timer.
func ScheduleApply(state *DebounceState, file string, applyCh chan string) {
	state.mutex.Lock()
	defer state.mutex.Unlock()

	state.lastFile = file
	state.generation++

	currentGen := state.generation

	if state.timer != nil {
		state.timer.Stop()
	}

	state.timer = time.AfterFunc(DebounceInterval, func() {
		EnqueueIfCurrent(state, currentGen, applyCh)
	})
}

// EnqueueIfCurrent checks whether the generation is still current and, if so,
// coalesces any stale pending apply and enqueues the latest file.
func EnqueueIfCurrent(state *DebounceState, expectedGen uint64, applyCh chan string) {
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

package timer

import "time"

// Timer tracks elapsed time for CLI command execution.
//
// Timer provides methods to start timing, mark stage transitions,
// and retrieve current timing information. It is designed for
// single-threaded CLI command execution.
type Timer interface {
	// Start initializes timing tracking. Sets both total and stage
	// start times to the current time. Can be called multiple times
	// to reset the timer.
	Start()

	// NewStage marks a stage transition.
	// Resets the stage timer while preserving total elapsed time.
	NewStage()

	// GetTiming returns the current elapsed durations.
	// Returns (total, stage) where:
	//   - total: time elapsed since Start()
	//   - stage: time elapsed since last NewStage() or Start()
	// Can be called multiple times without side effects.
	GetTiming() (total, stage time.Duration)

	// Stop signals completion of timing. This is optional and
	// provided for future extensibility. Currently a no-op.
	Stop()
}

// Impl is the concrete implementation of the Timer interface.
type Impl struct {
	startTime      time.Time
	stageStartTime time.Time
}

// New creates a new Timer instance.
// The timer must be started with Start() before use.
func New() *Impl {
	return &Impl{}
}

// Start initializes the timer and begins tracking elapsed time.
// Sets both total and stage start times to the current time.
// Can be called multiple times to reset the timer.
func (t *Impl) Start() {
	now := time.Now()
	t.startTime = now
	t.stageStartTime = now
}

// NewStage marks a transition to a new stage.
// Resets the stage timer while preserving total elapsed time.
func (t *Impl) NewStage() {
	t.stageStartTime = time.Now()
}

// GetTiming returns the current elapsed durations.
// Returns (total, stage) where:
//   - total: time elapsed since Start() was called
//   - stage: time elapsed since last NewStage() or Start()
//
// If Start() has not been called, returns (0, 0).
// Can be called multiple times without side effects.
func (t *Impl) GetTiming() (time.Duration, time.Duration) {
	// Handle case where Start() hasn't been called
	if t.startTime.IsZero() {
		return 0, 0
	}

	now := time.Now()
	total := now.Sub(t.startTime)
	stage := now.Sub(t.stageStartTime)

	return total, stage
}

// Stop signals the end of timing tracking.
// This is a no-op in the current implementation but provided
// for future extensibility (e.g., resource cleanup).
func (t *Impl) Stop() {
	// No-op: timer state remains accessible via GetTiming()
}

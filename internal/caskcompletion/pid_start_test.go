package caskcompletion_test

import (
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type completionPIDRead struct {
	PID   int
	Class string
}

type completionPIDStart struct {
	Read            completionPIDRead
	ProbeState      string
	ProbeErr        error
	DeadlineReached bool
}

// readCompletionPID retains fixed read/parse classes without retaining file contents.
func readCompletionPID(data []byte, err error) completionPIDRead {
	if errors.Is(err, fs.ErrNotExist) {
		return completionPIDRead{Class: "missing"}
	}

	if err != nil {
		return completionPIDRead{Class: "read-error"}
	}

	value := strings.TrimSpace(string(data))
	if value == "" {
		return completionPIDRead{Class: "empty"}
	}

	pid, err := strconv.Atoi(value)
	if err != nil {
		return completionPIDRead{Class: "invalid"}
	}

	if pid <= 1 {
		return completionPIDRead{Class: "not-child-pid"}
	}

	return completionPIDRead{PID: pid, Class: "valid"}
}

// waitCompletionPID observes probe completion alongside file polling. A deadline
// snapshots queued observations before cancellation can replace the original cause.
func waitCompletionPID(reads <-chan completionPIDRead, finished <-chan error,
	deadline <-chan time.Time,
) completionPIDStart {
	observed := completionPIDStart{
		Read:       completionPIDRead{Class: "unobserved"},
		ProbeState: "unobserved",
	}
	if finished != nil {
		observed.ProbeState = "pending"
	}

	for {
		if observed.takeProbeResult(finished) {
			return observed
		}

		select {
		case read, open := <-reads:
			if !open {
				reads = nil
				observed.Read.Class = "reader-closed"

				continue
			}

			observed.Read = read
			if read.PID > 1 {
				observed.checkDeadline(deadline)
				observed.takeProbeResult(finished)

				return observed
			}
		case err := <-finished:
			observed.ProbeState, observed.ProbeErr = "returned", err

			return observed
		case <-deadline:
			observed.DeadlineReached = true
			observed.takePIDRead(reads)
			observed.takeProbeResult(finished)

			return observed
		}
	}
}

// checkDeadline refuses a PID observed only after the startup budget expired.
func (observed *completionPIDStart) checkDeadline(deadline <-chan time.Time) {
	select {
	case <-deadline:
		observed.DeadlineReached = true
	default:
	}
}

// takePIDRead retains an already queued read without waiting beyond the deadline.
func (observed *completionPIDStart) takePIDRead(reads <-chan completionPIDRead) {
	select {
	case read, open := <-reads:
		if open {
			observed.Read = read
		} else {
			observed.Read.Class = "reader-closed"
		}
	default:
	}
}

func (observed *completionPIDStart) takeProbeResult(finished <-chan error) bool {
	select {
	case err := <-finished:
		observed.ProbeState, observed.ProbeErr = "returned", err

		return true
	default:
		return false
	}
}

func TestPIDStartupReportsEarlyProbeCompletion(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		err  error
	}{
		{"original probe error", fs.ErrPermission},
		{"unexpected nil result", nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			finished := make(chan error, 1)
			finished <- testCase.err

			deadline := make(chan time.Time, 1)
			deadline <- time.Time{}

			observed := waitCompletionPID(nil, finished, deadline)
			assert.Equal(t, "returned", observed.ProbeState)
			require.ErrorIs(t, observed.ProbeErr, testCase.err)
			assert.Zero(t, observed.Read.PID)
		})
	}
}

func TestPIDStartupReportsPendingDeadlineWithLastReadClass(t *testing.T) {
	t.Parallel()

	reads := make(chan completionPIDRead, 1)
	reads <- completionPIDRead{Class: "missing"}

	finished := make(chan error)

	deadline := make(chan time.Time, 1)
	deadline <- time.Time{}

	observed := waitCompletionPID(reads, finished, deadline)
	assert.True(t, observed.DeadlineReached)
	assert.Equal(t, "pending", observed.ProbeState)
	assert.Equal(t, "missing", observed.Read.Class)
	assert.Zero(t, observed.Read.PID)
	require.NoError(t, observed.ProbeErr)
}

func TestPIDStartupAcceptsObservedPIDWhileProbeIsPending(t *testing.T) {
	t.Parallel()

	reads := make(chan completionPIDRead, 1)
	reads <- completionPIDRead{PID: 42, Class: "valid"}

	observed := waitCompletionPID(reads, make(chan error), nil)
	assert.Equal(t, 42, observed.Read.PID)
	assert.Equal(t, "valid", observed.Read.Class)
	assert.Equal(t, "pending", observed.ProbeState)
	assert.False(t, observed.DeadlineReached)
	require.NoError(t, observed.ProbeErr)
}

func TestPIDReadRetainsReadAndParseClasses(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, data, class string
		err               error
		pid               int
	}{
		{name: "missing file", err: fs.ErrNotExist, class: "missing"},
		{name: "other read error", err: fs.ErrPermission, class: "read-error"},
		{name: "empty file", class: "empty"},
		{name: "invalid contents", data: "not-a-pid", class: "invalid"},
		{name: "not a child PID", data: "1\n", class: "not-child-pid"},
		{name: "valid child PID", data: "42\n", class: "valid", pid: 42},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			observed := readCompletionPID([]byte(testCase.data), testCase.err)
			assert.Equal(t, testCase.class, observed.Class)
			assert.Equal(t, testCase.pid, observed.PID)
		})
	}
}

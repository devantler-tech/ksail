package releasebuildprofile

import (
	"bufio"
	"io"
	"regexp"
	"strings"
	"time"
)

type phaseObservation struct {
	start    time.Time
	finish   time.Time
	duration time.Duration
}

// PhaseSeconds admits the complete binary phase, never a nested hook's receipt.
// Both boundaries are timestamped by the harness while streaming live output.
func PhaseSeconds(input io.Reader) (float64, error) {
	scanner := bufio.NewScanner(input)
	observation := phaseObservation{}
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	for scanner.Scan() {
		timestamp, line, ok := strings.Cut(scanner.Text(), "\t")
		if !ok {
			return 0, errInvalid
		}

		observed, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			return 0, errInvalid
		}

		err = observation.admit(observed, ansi.ReplaceAllString(line, ""))
		if err != nil {
			return 0, err
		}
	}

	if scanner.Err() != nil {
		return 0, errInvalid
	}

	return observation.seconds()
}

func (observation *phaseObservation) admit(observed time.Time, line string) error {
	if line == "  • building binaries" {
		if !observation.start.IsZero() {
			return errInvalid
		}

		observation.start = observed

		return nil
	}

	if observation.start.IsZero() || !observation.finish.IsZero() {
		return nil
	}

	if strings.HasPrefix(line, "  • ") {
		observation.finish = observed

		return nil
	}

	text, found := strings.CutPrefix(strings.TrimSpace(line), "• took: ")
	if !found {
		return nil
	}

	duration, err := time.ParseDuration(text)
	if err != nil {
		return errInvalid
	}

	observation.duration = duration

	return nil
}

func (observation *phaseObservation) seconds() (float64, error) {
	if observation.start.IsZero() || observation.finish.IsZero() || observation.duration <= 0 {
		return 0, errInvalid
	}

	// The human receipt is rounded. Allow two seconds plus bounded log latency.
	const rounding, latency = 2 * time.Second, 10 * time.Second

	window := observation.finish.Sub(observation.start)

	delta := window - observation.duration
	if delta < -rounding || delta > latency {
		return 0, errInvalid
	}

	return window.Seconds(), nil
}

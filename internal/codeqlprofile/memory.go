package codeqlprofile

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MemorySample is a point-in-time Linux observation, not a process-tree peak.
type MemorySample struct {
	TotalBytes      uint64
	AvailableBytes  uint64
	SwapTotalBytes  uint64
	SwapUsedBytes   uint64
	LargestRSSBytes uint64
}

// MemorySummary retains sampled extrema with their interval and observation age.
type MemorySummary struct {
	SampleCount            uint64  `json:"sampleCount"`
	LastElapsedSeconds     float64 `json:"lastElapsedSeconds"`
	TotalBytes             uint64  `json:"totalBytes"`
	SwapTotalBytes         uint64  `json:"swapTotalBytes"`
	MinimumAvailableBytes  uint64  `json:"minimumAvailableBytes"`
	MaximumSwapUsedBytes   uint64  `json:"maximumSwapUsedBytes"`
	LargestSampledRSSBytes uint64  `json:"largestSampledRssBytes"`
}

const (
	kibibyte               = 1024
	memoryFieldCount       = 4
	memoryFieldParts       = 3
	memorySampleInterval   = 5 * time.Second
	memoryProbeTimeout     = 2 * time.Second
	privateObservationMode = 0o600
)

// ParseMemory requires every native memory field and a successful ps RSS value.
// Missing, duplicated or impossible values never become zero-valued observations.
func ParseMemory(input, largestRSSKiB string) (MemorySample, error) {
	fields, err := memoryFields(input)
	if err != nil {
		return MemorySample{}, err
	}

	rss, err := parseKiB(strings.TrimSpace(largestRSSKiB))
	if err != nil || rss == 0 || !validMemoryFields(fields) {
		return MemorySample{}, errMeasurement
	}

	return MemorySample{
		TotalBytes:      fields["MemTotal:"],
		AvailableBytes:  fields["MemAvailable:"],
		SwapTotalBytes:  fields["SwapTotal:"],
		SwapUsedBytes:   fields["SwapTotal:"] - fields["SwapFree:"],
		LargestRSSBytes: rss,
	}, nil
}

func memoryFields(input string) (map[string]uint64, error) {
	fields := make(map[string]uint64, memoryFieldCount)

	for line := range strings.SplitSeq(input, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "MemTotal:", "MemAvailable:", "SwapTotal:", "SwapFree:":
			if len(parts) != memoryFieldParts || parts[2] != "kB" {
				return nil, errMeasurement
			}

			if _, duplicate := fields[parts[0]]; duplicate {
				return nil, errMeasurement
			}

			value, err := parseKiB(parts[1])
			if err != nil {
				return nil, err
			}

			fields[parts[0]] = value
		}
	}

	return fields, nil
}

func validMemoryFields(fields map[string]uint64) bool {
	return len(fields) == memoryFieldCount && fields["MemTotal:"] > 0 &&
		fields["MemAvailable:"] <= fields["MemTotal:"] && fields["SwapFree:"] <= fields["SwapTotal:"]
}

func parseKiB(input string) (uint64, error) {
	value, err := strconv.ParseUint(input, 10, 64)
	if err != nil || value > math.MaxUint64/kibibyte {
		return 0, errMeasurement
	}

	return value * kibibyte, nil
}

// ObserveMemory rejects a changing envelope or unordered timestamps. Extrema are
// samples only; neither their maxima nor native command RSS sum concurrent usage.
func ObserveMemory(
	previous MemorySummary,
	elapsed float64,
	sample MemorySample,
) (MemorySummary, error) {
	if !validElapsed(elapsed) || !validMemorySample(sample) ||
		previous.SampleCount == math.MaxUint64 {
		return previous, errMeasurement
	}

	if previous.SampleCount == 0 {
		previous.TotalBytes = sample.TotalBytes
		previous.SwapTotalBytes = sample.SwapTotalBytes
		previous.MinimumAvailableBytes = sample.AvailableBytes
	} else if !sameMemoryEnvelope(previous, elapsed, sample) {
		return previous, errMeasurement
	}

	previous.SampleCount++
	previous.LastElapsedSeconds = elapsed
	previous.MinimumAvailableBytes = min(previous.MinimumAvailableBytes, sample.AvailableBytes)
	previous.MaximumSwapUsedBytes = max(previous.MaximumSwapUsedBytes, sample.SwapUsedBytes)
	previous.LargestSampledRSSBytes = max(previous.LargestSampledRSSBytes, sample.LargestRSSBytes)

	return previous, nil
}

func validElapsed(elapsed float64) bool {
	return !math.IsNaN(elapsed) && !math.IsInf(elapsed, 0) && elapsed >= 0
}

func validMemorySample(sample MemorySample) bool {
	return sample.TotalBytes > 0 && sample.AvailableBytes <= sample.TotalBytes &&
		sample.SwapUsedBytes <= sample.SwapTotalBytes && sample.LargestRSSBytes > 0
}

func sameMemoryEnvelope(previous MemorySummary, elapsed float64, sample MemorySample) bool {
	return elapsed > previous.LastElapsedSeconds && sample.TotalBytes == previous.TotalBytes &&
		sample.SwapTotalBytes == previous.SwapTotalBytes
}

// SampleMemory publishes atomic five-second snapshots until cancellation. It
// exits unsuccessfully on a failed observation or when its owning shell exits.
func SampleMemory(ctx context.Context, output string) error {
	start := time.Now()
	parent := os.Getppid()

	ticker := time.NewTicker(memorySampleInterval)
	defer ticker.Stop()

	var summary MemorySummary

	for {
		sample, err := systemMemory(ctx)
		if err != nil {
			if ctx.Err() != nil && summary.SampleCount > 0 {
				return nil
			}

			return err
		}

		summary, err = ObserveMemory(summary, time.Since(start).Seconds(), sample)
		if err != nil {
			return err
		}

		err = publishMemory(output, summary)
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if os.Getppid() != parent {
				return errMeasurement
			}
		}
	}
}

func publishMemory(output string, summary MemorySummary) error {
	data, err := json.Marshal(summary)
	if err != nil {
		return errMeasurement
	}

	err = os.WriteFile(output+".tmp", append(data, '\n'), privateObservationMode)
	if err != nil {
		return errMeasurement
	}

	err = os.Rename(output+".tmp", output)
	if err != nil {
		return errMeasurement
	}

	return nil
}

func systemMemory(ctx context.Context) (MemorySample, error) {
	contents, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemorySample{}, errMeasurement
	}

	ctx, cancel := context.WithTimeout(ctx, memoryProbeTimeout)
	defer cancel()

	rss, err := exec.CommandContext(ctx, "ps", "-eo", "rss=", "--sort=-rss").Output()
	if err != nil {
		return MemorySample{}, errMeasurement
	}

	values := strings.Fields(string(rss))
	if len(values) == 0 {
		return MemorySample{}, errMeasurement
	}

	return ParseMemory(string(contents), values[0])
}

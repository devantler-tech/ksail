// Package codeqlprofile validates extraction command resource measurements.
// A complete measurement report is separate from the CodeQL analysis verdict.
package codeqlprofile

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	errMeasurement = errors.New("incomplete or invalid resource measurement")
	errInventory   = errors.New("incomplete or invalid extraction inventory")
	projectName    = regexp.MustCompile(`^[a-z0-9][a-z0-9_/-]*$`)
)

const (
	linuxPlatform    = "linux"
	secondsPerMinute = 60
)

// Stats describes the measured command and its children, rather than the Go heap
// or the sum of concurrent process RSS. GNU time supplies its own exit status.
type Stats struct {
	CommandPeakRSSBytes uint64  `json:"commandPeakRssBytes"`
	WallSeconds         float64 `json:"wallSeconds"`
	ExitCode            int     `json:"timeExitCode"`
}

// Record binds the process status and its measurements to a requested extraction.
type Record struct {
	Project  string `json:"project"`
	ExitCode int    `json:"exitCode"`
	Stats    Stats  `json:"stats"`
}

// Report retains failed and partial observations for investigation.
type Report struct {
	Complete bool     `json:"complete"`
	Expected []string `json:"expectedProjects"`
	Records  []Record `json:"records"`
}

// ParseTime reads GNU time on Linux or BSD time on macOS. Missing or repeated
// fields are rejected instead of producing plausible zero-valued measurements.
func ParseTime(platform, input string) (Stats, error) {
	fields, err := timeFields(platform, input)
	if err != nil {
		return Stats{}, err
	}

	unit := uint64(1)
	if platform == linuxPlatform {
		unit = 1024
	}

	rss, err := strconv.ParseUint(fields["rss"], 10, 64)
	if err != nil || rss == 0 || rss > math.MaxUint64/unit {
		return Stats{}, errMeasurement
	}

	seconds, err := parseDuration(fields["elapsed"], platform)
	if err != nil {
		return Stats{}, err
	}

	stats := Stats{CommandPeakRSSBytes: rss * unit, WallSeconds: seconds}

	if platform == linuxPlatform {
		code, parseErr := strconv.ParseUint(fields["status"], 10, 8)
		if parseErr != nil {
			return stats, errMeasurement
		}

		stats.ExitCode = int(code)
	}

	return stats, nil
}

func timeFields(platform, input string) (map[string]string, error) {
	fields := make(map[string]string)

	for line := range strings.SplitSeq(input, "\n") {
		key, value := timeField(platform, strings.TrimSpace(line))
		if key == "" {
			continue
		}

		if _, exists := fields[key]; exists {
			return nil, errMeasurement
		}

		fields[key] = value
	}

	if fields["rss"] == "" || fields["elapsed"] == "" ||
		(platform == linuxPlatform && fields["status"] == "") {
		return nil, errMeasurement
	}

	return fields, nil
}

func timeField(platform, line string) (string, string) {
	if platform == linuxPlatform {
		for _, field := range []struct{ prefix, key string }{
			{"Elapsed (wall clock) time (h:mm:ss or m:ss):", "elapsed"},
			{"Maximum resident set size (kbytes):", "rss"},
			{"Exit status:", "status"},
		} {
			if value, found := strings.CutPrefix(line, field.prefix); found {
				return field.key, strings.TrimSpace(value)
			}
		}
	}

	if platform == "darwin" {
		return darwinTimeField(line)
	}

	return "", ""
}

func darwinTimeField(line string) (string, string) {
	parts := strings.Fields(line)
	if len(parts) == 6 && parts[1] == "real" && parts[3] == "user" && parts[5] == "sys" {
		return "elapsed", parts[0]
	}

	if len(parts) == 5 && strings.Join(parts[1:], " ") == "maximum resident set size" {
		return "rss", parts[0]
	}

	return "", ""
}

func parseDuration(input, platform string) (float64, error) {
	parts := strings.Split(input, ":")
	if (platform == linuxPlatform && len(parts) != 2 && len(parts) != 3) ||
		(platform == "darwin" && len(parts) != 1) {
		return 0, errMeasurement
	}

	var total float64

	for i, part := range parts {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil || invalidDurationPart(value, i, len(parts)) {
			return 0, errMeasurement
		}

		total = total*secondsPerMinute + value
	}

	if math.IsInf(total, 0) {
		return 0, errMeasurement
	}

	return total, nil
}

func invalidDurationPart(value float64, index, count int) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value < 0 ||
		(index > 0 && value >= secondsPerMinute) || (index < count-1 && math.Trunc(value) != value)
}

// Summarize accepts only an exact inventory of successful, measured processes.
// On failure the returned report still contains every original observation.
func Summarize(expected []string, records []Record) (Report, error) {
	report := Report{Expected: expected, Records: records}
	if len(expected) == 0 {
		return report, errInventory
	}

	wanted, err := inventory(expected)
	if err != nil {
		return report, err
	}

	observed := make(map[string]Record, len(records))
	for _, record := range records {
		_, duplicate := observed[record.Project]
		if !wanted[record.Project] || duplicate || invalidRecord(record) {
			return report, errMeasurement
		}

		observed[record.Project] = record
	}

	if len(observed) != len(expected) {
		return report, errInventory
	}

	report.Records = make([]Record, 0, len(expected))
	for _, name := range expected {
		report.Records = append(report.Records, observed[name])
	}

	report.Complete = true

	return report, nil
}

func inventory(expected []string) (map[string]bool, error) {
	wanted := make(map[string]bool, len(expected))

	for _, name := range expected {
		if !projectName.MatchString(name) || wanted[name] {
			return nil, errInventory
		}

		wanted[name] = true
	}

	return wanted, nil
}

func invalidRecord(record Record) bool {
	stats := record.Stats

	return record.ExitCode != 0 || stats.ExitCode != 0 || stats.CommandPeakRSSBytes == 0 ||
		math.IsNaN(stats.WallSeconds) || math.IsInf(stats.WallSeconds, 0) || stats.WallSeconds < 0
}

package codeqlprofile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadMeasurements binds numerically indexed time and exit files to the requested
// inventory. Project names never choose filesystem paths. Missing or malformed
// observations cannot become success; every collected process is retained.
func ReadMeasurements(platform, directory string, expected []string) (Report, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Report{Expected: expected}, errMeasurement
	}

	defer func() { _ = root.Close() }()

	records := make([]Record, 0, len(expected))
	complete := true

	for index, project := range expected {
		base := fmt.Sprintf("%05d", index)

		record, readErr := readMeasurement(platform, root, base, project)
		if readErr != nil {
			complete = false
		}

		if record.Project != "" {
			records = append(records, record)
		}
	}

	report, err := Summarize(expected, records)
	if !complete {
		report.Complete = false

		return report, errMeasurement
	}

	return report, err
}

func readMeasurement(platform string, root *os.Root, base, project string) (Record, error) {
	status, err := root.ReadFile(base + ".exit")
	if err != nil {
		return Record{}, errMeasurement
	}

	code, err := strconv.ParseUint(strings.TrimSpace(string(status)), 10, 8)
	if err != nil {
		return Record{Project: project, ExitCode: -1}, errMeasurement
	}

	record := Record{Project: project, ExitCode: int(code)}

	measurement, err := root.ReadFile(base + ".time")
	if err != nil {
		return record, errMeasurement
	}

	record.Stats, err = ParseTime(platform, string(measurement))

	return record, err
}

// Command cmd emits a sanitized CodeQL extraction resource report. A failed or
// partial collection still emits JSON and exits unsuccessfully.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
)

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout))
}

func run(args []string, input io.Reader, output io.Writer) int {
	const cgroupArguments, countArguments = 4, 3
	if len(args) == cgroupArguments && args[1] == "--cgroup-limits" {
		limits, err := codeqlprofile.ReadCgroupLimits(args[2], args[3])

		return emitObservation(output, limits, err)
	}

	if len(args) == countArguments && args[1] == "--count-extracted-files" {
		count, err := codeqlprofile.CountExtractedFiles(args[2], input)

		return emitObservation(output, count, err)
	}

	return reportResources(args, input, output)
}

func emitObservation(output io.Writer, value any, observationErr error) int {
	if observationErr != nil {
		fmt.Fprintln(os.Stderr, "native observation is incomplete or invalid")

		return 1
	}

	err := json.NewEncoder(output).Encode(value)
	if err != nil {
		return 1
	}

	return 0
}

func reportResources(args []string, input io.Reader, output io.Writer) int {
	const (
		argumentCount     = 2
		usageExit         = 2
		maxInventoryBytes = 64 * 1024
	)

	if len(args) != argumentCount {
		fmt.Fprintln(os.Stderr, "usage: codeql-profile METRICS_DIRECTORY < INVENTORY")

		return usageExit
	}

	contents, err := io.ReadAll(io.LimitReader(input, maxInventoryBytes+1))
	if err != nil || len(contents) > maxInventoryBytes {
		fmt.Fprintln(os.Stderr, "resource inventory is unavailable")

		return 1
	}

	expected := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	report, collectionErr := codeqlprofile.ReadMeasurements(runtime.GOOS, args[1], expected)
	err = json.NewEncoder(output).Encode(report)

	if err != nil || collectionErr != nil {
		fmt.Fprintln(os.Stderr, "resource measurements are incomplete or invalid")

		return 1
	}

	return 0
}

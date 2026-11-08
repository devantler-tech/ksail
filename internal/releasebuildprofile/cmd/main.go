// Command cmd validates one fixed, non-publishing release-build experiment.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
	"github.com/devantler-tech/ksail/v7/internal/releasebuildprofile"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

func main() {
	code := run(os.Args[1:], os.Stdout)
	if code != 0 {
		fmt.Fprintln(os.Stderr, "release experiment is incomplete or incompatible")
	}

	os.Exit(code)
}

func status(err error) int {
	if err != nil {
		return 1
	}

	return 0
}

func run(args []string, output io.Writer) int {
	const configArguments, comparisonArguments, inspectionArguments = 4, 3, 2

	err := releasebuildprofile.CheckCredentials()
	if err != nil || len(args) == 0 {
		return 1
	}

	commands := map[string]struct {
		arguments int
		invoke    func([]string) int
	}{
		"runtime": {1, func(_ []string) int {
			return status(json.NewEncoder(output).Encode(map[string]any{
				"goVersion": runtime.Version(), "defaultChildProcs": runtime.GOMAXPROCS(0),
			}))
		}},
		"limits":         {1, func(_ []string) int { return limits(output) }},
		"prepare-config": {configArguments, prepareConfig},
		"compare":        {comparisonArguments, compare},
		"inspect": {
			inspectionArguments,
			func(values []string) int { return inspect(values[1], output) },
		},
		"phase": {
			inspectionArguments,
			func(values []string) int { return phase(values[1], output) },
		},
	}

	command, exists := commands[args[0]]
	if !exists || len(args) != command.arguments {
		return 1
	}

	return command.invoke(args)
}

func limits(output io.Writer) int {
	limits, err := codeqlprofile.ReadCgroupLimits("/proc", "/sys/fs/cgroup")
	if err != nil {
		return 1
	}

	return status(json.NewEncoder(output).Encode(limits))
}

func inspect(directory string, output io.Writer) int {
	outputs, err := releasebuildprofile.InspectOutputs(directory)
	if err != nil {
		return 1
	}

	return status(json.NewEncoder(output).Encode(outputs))
}

func prepareConfig(args []string) int {
	data, err := fsutil.ReadFileSafe(filepath.Dir(args[2]), args[2])
	if err != nil {
		return 1
	}

	configured, err := releasebuildprofile.PrepareConfig(data, args[1])
	if err != nil {
		return 1
	}

	return status(releasebuildprofile.WriteFile(args[3], configured))
}

func compare(args []string) int {
	expected := releasebuildprofile.Expected{
		HarnessSHA: os.Getenv(
			"GITHUB_SHA",
		),
		RunID:   os.Getenv("GITHUB_RUN_ID"),
		Attempt: os.Getenv("GITHUB_RUN_ATTEMPT"),
	}
	result, comparisonErr := releasebuildprofile.CompareFiles(args[1], expected)

	err := releasebuildprofile.WriteJSON(args[2], result)
	if err != nil {
		return 1
	}

	return status(comparisonErr)
}

func phase(filename string, output io.Writer) int {
	data, err := fsutil.ReadFileSafe(filepath.Dir(filename), filename)
	if err != nil {
		return 1
	}

	seconds, err := releasebuildprofile.PhaseSeconds(bytes.NewReader(data))
	if err != nil {
		return 1
	}

	return status(json.NewEncoder(output).Encode(seconds))
}

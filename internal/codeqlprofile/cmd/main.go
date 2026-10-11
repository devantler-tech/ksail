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
	const (
		argumentCount     = 2
		usageExit         = 2
		maxInventoryBytes = 64 * 1024
	)

	if len(os.Args) != argumentCount {
		fmt.Fprintln(os.Stderr, "usage: codeql-profile METRICS_DIRECTORY < INVENTORY")
		os.Exit(usageExit)
	}

	contents, err := io.ReadAll(io.LimitReader(os.Stdin, maxInventoryBytes+1))
	if err != nil || len(contents) > maxInventoryBytes {
		fmt.Fprintln(os.Stderr, "resource inventory is unavailable")
		os.Exit(1)
	}

	expected := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	report, collectionErr := codeqlprofile.ReadMeasurements(runtime.GOOS, os.Args[1], expected)
	err = json.NewEncoder(os.Stdout).Encode(report)

	if err != nil || collectionErr != nil {
		fmt.Fprintln(os.Stderr, "resource measurements are incomplete or invalid")
		os.Exit(1)
	}
}

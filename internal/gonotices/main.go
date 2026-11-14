package gonotices

import (
	"fmt"
	"io"
)

// FailureMessage starts every failure line the release helper prints.
const FailureMessage = "Go recipient notice verification failed"

// Main runs Run and returns the process exit code. On failure it prints
// FailureMessage followed by the underlying cause to stderr, so a red release
// or CI job shows why the check failed. The errors Run returns name only
// paths, targets, modules and versions.
func Main(root string, arguments []string, stdout, stderr io.Writer) int {
	err := Run(root, arguments, stdout)
	if err == nil {
		return 0
	}

	_, _ = fmt.Fprintf(stderr, "%s: %v\n", FailureMessage, err)

	return 1
}

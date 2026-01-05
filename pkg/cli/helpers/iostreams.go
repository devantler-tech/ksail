package helpers

import (
	"os"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewStandardIOStreams creates IOStreams for standard input/output/error.
// This is used by kubectl-based commands across the CLI.
func NewStandardIOStreams() genericiooptions.IOStreams {
	return genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}
}

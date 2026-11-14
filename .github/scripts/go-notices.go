// Go recipient-material staging and archive verification for existing release jobs.
package main

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", gonotices.FailureMessage, err)
		os.Exit(1)
	}

	os.Exit(gonotices.Main(root, os.Args[1:], os.Stdout, os.Stderr))
}

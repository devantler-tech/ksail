// Go recipient-material staging and archive verification for existing release jobs.
package main

import (
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
)

func main() {
	root, err := os.Getwd()
	if err == nil {
		err = gonotices.Run(root, os.Args[1:], os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Go recipient notice verification failed")
		os.Exit(1)
	}
}

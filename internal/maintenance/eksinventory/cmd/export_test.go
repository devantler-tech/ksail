package main

import "io"

// RunForTest exposes argument handling to external package tests.
func RunForTest(args []string, output io.Writer) int {
	return run(args, output)
}

// Command memory records Linux memory observations for the owned resource profile.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
)

func main() {
	os.Exit(run())
}

func run() int {
	const (
		argumentCount, usageExit = 2, 2
		maximumDuration          = 110 * time.Minute
	)

	if len(os.Args) != argumentCount {
		fmt.Fprintln(os.Stderr, "usage: codeql-profile-memory OUTPUT")

		return usageExit
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, maximumDuration)
	defer cancel()

	err := codeqlprofile.SampleMemory(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "native memory observation or publication failed")

		return 1
	}

	return 0
}

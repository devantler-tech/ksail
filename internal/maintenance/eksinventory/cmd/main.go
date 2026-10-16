// Command eksinventory prints a read-only regional AWS resource diagnostic.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

const (
	usageExitCode = 2
	reportTimeout = 2 * time.Minute
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

func run(args []string, output io.Writer) int {
	flags := flag.NewFlagSet("eksinventory", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	region := flags.String("region", "", "AWS region to read")

	var baseline baselineOptions
	baseline.register(flags)

	err := flags.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(
				output,
				"Usage: report-eks-account-capacity.sh --region REGION\nIt only reads and it creates nothing.",
			)

			return 0
		}

		return usageExitCode
	}

	if flags.NArg() != 0 {
		return usageExitCode
	}

	ctx, cancel := context.WithTimeout(context.Background(), reportTimeout)
	defer cancel()

	if baseline.requested(flags) {
		baseline.binding.Region = *region
		if baseline.destination == "" || baseline.binding.Validate() != nil {
			return usageExitCode
		}

		err := eksinventory.CaptureBaseline(
			ctx,
			baseline.binding,
			baseline.destination,
			output,
			nil,
		)
		if err != nil {
			_, _ = fmt.Fprintln(
				output,
				"Baseline capture failed: all observations must complete and the destination must be new and private.",
			)

			return 1
		}

		return 0
	}

	err = eksinventory.Report(ctx, *region, output, nil)
	if err != nil {
		return usageExitCode
	}

	return 0
}

type baselineOptions struct {
	destination string
	binding     eksinventory.BaselineContext
}

func (options *baselineOptions) register(flags *flag.FlagSet) {
	flags.StringVar(&options.destination, "baseline-file", "", "New private regional baseline file")
	flags.StringVar(&options.binding.ClusterName, "cluster-name", "", "Observation cluster label")
	flags.StringVar(&options.binding.RunID, "run-id", "", "Observation workflow run ID")
	flags.StringVar(
		&options.binding.RunAttempt,
		"run-attempt",
		"",
		"Observation workflow run attempt",
	)
	flags.StringVar(&options.binding.SourceSHA, "source-sha", "", "Observation source commit")
}

func (options *baselineOptions) requested(flags *flag.FlagSet) bool {
	requested := false

	flags.Visit(func(value *flag.Flag) {
		if value.Name != "region" {
			requested = true
		}
	})

	return requested
}

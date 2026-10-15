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

	err = eksinventory.Report(ctx, *region, output, nil)
	if err != nil {
		return usageExitCode
	}

	return 0
}

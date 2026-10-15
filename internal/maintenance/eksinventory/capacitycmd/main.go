// Command capacitycmd checks regional quota without creating AWS resources.
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
	usageExitCode   = 2
	maxPlanBytes    = 4 << 20
	capacityTimeout = 2 * time.Minute
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout)) }

func run(args []string, input io.Reader, output io.Writer) int {
	flags := flag.NewFlagSet("check-eks-capacity", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	region := flags.String("region", "", "selected AWS region")
	cluster := flags.String("cluster-name", "", "expected cluster identity")

	peak := flags.Int("peak-nodes", 0, "scenario peak node count")

	err := flags.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(
				output,
				"Usage: check-eks-capacity.sh --region REGION --cluster-name NAME --peak-nodes N < eks.json",
			)

			return 0
		}

		return usageExitCode
	}

	if flags.NArg() != 0 || *region == "" || *cluster == "" || *peak < 1 {
		return usageExitCode
	}

	config, err := io.ReadAll(io.LimitReader(input, maxPlanBytes+1))
	if err != nil {
		_, _ = fmt.Fprintln(output, "EKS capacity: unknown (plan-read-failed); creation refused")

		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), capacityTimeout)
	defer cancel()

	err = eksinventory.CheckCapacity(
		ctx,
		*region,
		*cluster,
		*peak,
		config,
		output,
		nil,
	)
	if err != nil {
		return 1
	}

	return 0
}

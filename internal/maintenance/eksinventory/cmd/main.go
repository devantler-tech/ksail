// Command eksinventory prints a read-only regional AWS resource diagnostic.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

const (
	usageExitCode    = 2
	reportTimeout    = 2 * time.Minute
	ownershipTimeout = 5 * time.Minute
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

//nolint:cyclop,funlen // The command has three modes with distinct validation and exit semantics.
func run(args []string, output io.Writer) int {
	flags := flag.NewFlagSet("eksinventory", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	region := flags.String("region", "", "AWS region to read")

	var baseline baselineOptions
	baseline.register(flags)

	var ownership ownershipOptions
	ownership.register(flags)

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

	if ownership.destination != "" {
		baseline.binding.Region = *region
		if baseline.destination == "" || baseline.binding.Validate() != nil {
			return usageExitCode
		}

		ctx, cancel := context.WithTimeout(context.Background(), ownershipTimeout)
		defer cancel()

		err := eksinventory.CaptureOwnership(
			ctx,
			baseline.binding,
			baseline.destination,
			ownership.destination,
			eksinventory.OwnershipHints{
				ClassicLoadBalancers: ownership.classicLoadBalancers,
				LoadBalancerARNs:     ownership.loadBalancerARNs,
				LoadBalancerHosts:    ownership.loadBalancerHosts,
				VolumeIDs:            ownership.volumeIDs,
				WorkloadReadComplete: ownership.workloadReadComplete,
			},
			output,
			nil,
		)
		if err != nil {
			_, _ = fmt.Fprintln(
				output,
				"Ownership capture failed: private evidence is incomplete or does not match this run.",
			)

			return 1
		}

		return 0
	}

	if baseline.requested(flags) {
		baseline.binding.Region = *region
		if baseline.destination == "" || baseline.binding.Validate() != nil {
			return usageExitCode
		}

		ctx, cancel := context.WithTimeout(context.Background(), reportTimeout)
		defer cancel()

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

	ctx, cancel := context.WithTimeout(context.Background(), reportTimeout)
	defer cancel()

	err = eksinventory.Report(ctx, *region, output, nil)
	if err != nil {
		return usageExitCode
	}

	return 0
}

type ownershipOptions struct {
	destination          string
	classicLoadBalancers stringList
	loadBalancerARNs     stringList
	loadBalancerHosts    stringList
	volumeIDs            stringList
	workloadReadComplete bool
}

func (options *ownershipOptions) register(flags *flag.FlagSet) {
	flags.StringVar(&options.destination, "ownership-file", "", "Private cumulative ownership file")
	flags.Var(
		&options.classicLoadBalancers,
		"classic-load-balancer",
		"Exact workload Classic ELB name",
	)
	flags.Var(&options.loadBalancerARNs, "load-balancer-arn", "Exact workload ELBv2 ARN")
	flags.Var(
		&options.loadBalancerHosts,
		"load-balancer-host",
		"Exact workload load balancer DNS name",
	)
	flags.Var(&options.volumeIDs, "volume-id", "Exact workload EBS volume ID")
	flags.BoolVar(
		&options.workloadReadComplete,
		"workload-inventory-complete",
		false,
		"Kubernetes workload identity inventory completed",
	)
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	*values = append(*values, value)

	return nil
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

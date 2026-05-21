// Package operator provides the hidden `ksail operator` command that runs the KSail
// Kubernetes operator inside a hub cluster.
package operator

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	operatorsvc "github.com/devantler-tech/ksail/v7/pkg/operator"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NewOperatorCmd creates the `ksail operator` command.
func NewOperatorCmd(_ *di.Runtime) *cobra.Command {
	var opts operatorsvc.Options

	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Run the KSail Kubernetes operator",
		Long: `Run the KSail Kubernetes operator.

The operator runs inside a hub Kubernetes cluster, watches Cluster custom resources, and
continuously reconciles them by creating, updating, and deleting the underlying clusters.
It is intended to be deployed via the KSail operator Helm chart rather than run directly.`,
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			// SetupSignalHandler returns a context cancelled on SIGINT/SIGTERM for graceful shutdown.
			err := operatorsvc.Run(ctrl.SetupSignalHandler(), opts)
			if err != nil {
				return fmt.Errorf("run operator: %w", err)
			}

			return nil
		},
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	cmd.Flags().StringVar(
		&opts.MetricsBindAddress,
		"metrics-bind-address",
		"0",
		"Address the metrics endpoint binds to (\"0\" disables it)",
	)
	cmd.Flags().StringVar(
		&opts.HealthProbeBindAddress,
		"health-probe-bind-address",
		":8081",
		"Address the health and readiness probes bind to",
	)
	cmd.Flags().BoolVar(
		&opts.LeaderElection,
		"leader-elect",
		false,
		"Enable leader election to ensure only one active operator instance",
	)
	cmd.Flags().StringVar(
		&opts.APIBindAddress,
		"api-bind-address",
		"",
		"Address the REST API binds to (empty disables it, e.g. \":8080\")",
	)
	cmd.Flags().BoolVar(
		&opts.ReadOnly,
		"read-only",
		false,
		"Run the REST API in read-only mode, rejecting all mutating requests",
	)

	return cmd
}

package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/spf13/cobra"
)

// NewForgetCmd creates the explicit local-only recovery command for stale connections.
func NewForgetCmd() *cobra.Command {
	var kubeconfigPath, contextName string

	cmd := &cobra.Command{
		Use:   "forget",
		Short: "Forget one local kubeconfig context without contacting a cluster",
		Long: `Remove an exact context from one explicitly selected kubeconfig file.

Use this local-only recovery after reviewing an incomplete cluster deletion. It does not
check whether a cluster still exists or delete any cluster resources. Ownership and API
errors from cluster delete remain errors: forgetting a connection does not resolve them.

Cluster and user entries are removed only when no remaining context in the selected file
references them. Other contexts and shared entries are preserved. No current context or
KUBECONFIG environment default is selected implicitly.`,
		Example: `  ksail cluster forget --kubeconfig ./nested.config --context kind-nested --experimental`,
		Args:    cobra.NoArgs,
		// Recovery must bypass the root's connection refresh even before Cobra
		// validates required flags or the experimental guard runs.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			changed, err := k8s.ForgetContext(cmd.Context(), kubeconfigPath, contextName)
			if err != nil {
				return fmt.Errorf("forget local connection: %w", err)
			}

			if changed {
				cmd.Printf(
					"Forgot local context %q. Cluster resources were not checked or changed.\n",
					contextName,
				)
			} else {
				cmd.Printf("Local context %q is already absent. No changes made.\n", contextName)
			}

			return nil
		},
	}
	cmd.Flags().
		StringVar(&kubeconfigPath, "kubeconfig", "", "one explicit kubeconfig file to update locally")
	cmd.Flags().StringVar(&contextName, "context", "", "exact local context to forget")
	_ = cmd.MarkFlagRequired("kubeconfig")
	_ = cmd.MarkFlagRequired("context")

	return experimental.Guard(cmd)
}

package cluster

import (
	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/spf13/cobra"
)

// NewInfoCmd creates the cluster info command.
// The command queries the infrastructure provider API first, then attempts
// kubectl cluster-info, and only fails if no information is available at all.
func NewInfoCmd() *cobra.Command {
	var (
		nameFlag     string
		providerFlag v1alpha1.Provider
	)

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Display cluster information",
		Long: "Display cluster information from the infrastructure provider" +
			" and Kubernetes API. Succeeds if information is available from any source.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInfoCmd(cmd, nameFlag, providerFlag)
		},
	}

	lifecycle.BindNameAndProviderFlags(cmd, &nameFlag, &providerFlag)

	return cmd
}

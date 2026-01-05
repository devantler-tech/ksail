package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewRolloutCmd creates the workload rollout command.
func NewRolloutCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateRolloutCommand(kubeconfigPath)
	})
}

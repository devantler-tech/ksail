package workload

import (
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewForwardCmd creates the workload forward command.
func NewForwardCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreatePortForwardCommand(kubeconfigPath)
	})
}

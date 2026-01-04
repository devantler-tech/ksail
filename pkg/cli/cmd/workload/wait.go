package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewWaitCmd creates the workload wait command.
func NewWaitCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateWaitCommand(kubeconfigPath)
	})
}

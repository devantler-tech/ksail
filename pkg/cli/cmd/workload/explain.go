package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewExplainCmd creates the workload explain command.
func NewExplainCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateExplainCommand(kubeconfigPath)
	})
}

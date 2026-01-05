package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewDescribeCmd creates the workload describe command.
func NewDescribeCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateDescribeCommand(kubeconfigPath)
	})
}

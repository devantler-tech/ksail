package workload

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the workload delete command.
func NewDeleteCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateDeleteCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}

	return cmd
}

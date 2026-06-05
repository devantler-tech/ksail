package workload

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewExecCmd creates the workload exec command.
func NewExecCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateExecCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}

	return cmd
}

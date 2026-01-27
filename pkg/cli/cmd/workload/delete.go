package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the workload delete command.
func NewDeleteCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateDeleteCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewApplyCmd creates the workload apply command.
func NewApplyCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateApplyCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

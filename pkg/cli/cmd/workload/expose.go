package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewExposeCmd creates the workload expose command.
func NewExposeCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateExposeCommand(kubeconfigPath)
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}

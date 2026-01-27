package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewRolloutCmd creates the workload rollout command.
func NewRolloutCmd() *cobra.Command {
	cmd := newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateRolloutCommand(kubeconfigPath)
	})

	// Add permission annotation
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}

	cmd.Annotations[annotations.AnnotationPermission] = "write"

	return cmd
}

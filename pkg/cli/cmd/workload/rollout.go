package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewRolloutCmd creates the workload rollout command.
func NewRolloutCmd() *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := kubeconfig.GetPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the rollout command directly
	client := kubectl.NewClient(ioStreams)
	rolloutCmd := client.CreateRolloutCommand(kubeconfigPath)

	return rolloutCmd
}

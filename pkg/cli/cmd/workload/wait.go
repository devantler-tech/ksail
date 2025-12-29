package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewWaitCmd creates the workload wait command.
func NewWaitCmd() *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := kubeconfig.GetPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the wait command directly
	client := kubectl.NewClient(ioStreams)
	waitCmd := client.CreateWaitCommand(kubeconfigPath)

	return waitCmd
}

package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewScaleCmd creates the workload scale command.
func NewScaleCmd() *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := helpers.GetKubeconfigPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the scale command directly
	client := kubectl.NewClient(ioStreams)
	scaleCmd := client.CreateScaleCommand(kubeconfigPath)

	return scaleCmd
}

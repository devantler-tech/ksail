package workload

import (
	"os"

	"github.com/devantler-tech/ksail/pkg/client/kubectl"
	cmdhelpers "github.com/devantler-tech/ksail/pkg/cmd"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewEditCmd creates the workload edit command.
func NewEditCmd() *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := cmdhelpers.GetKubeconfigPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the edit command directly
	client := kubectl.NewClient(ioStreams)
	editCmd := client.CreateEditCommand(kubeconfigPath)

	return editCmd
}

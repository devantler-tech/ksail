package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	cmdhelpers "github.com/devantler-tech/ksail/v5/pkg/cmd"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewDeleteCmd creates the workload delete command.
func NewDeleteCmd() *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := cmdhelpers.GetKubeconfigPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the delete command directly
	client := kubectl.NewClient(ioStreams)
	deleteCmd := client.CreateDeleteCommand(kubeconfigPath)

	return deleteCmd
}

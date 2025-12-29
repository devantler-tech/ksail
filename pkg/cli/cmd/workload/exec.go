package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewExecCmd creates the workload exec command.
func NewExecCmd() *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := helpers.GetKubeconfigPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the exec command directly
	client := kubectl.NewClient(ioStreams)
	execCmd := client.CreateExecCommand(kubeconfigPath)

	return execCmd
}

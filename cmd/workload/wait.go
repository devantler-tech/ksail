package workload

import (
	"os"

	"github.com/devantler-tech/ksail-go/pkg/client/kubectl"
	"github.com/devantler-tech/ksail-go/pkg/cmd"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewWaitCmd creates the workload wait command.
// The runtime parameter is kept for consistency with other workload command constructors,
// though it's currently unused as this command wraps kubectl directly.
func NewWaitCmd(_ *runtime.Runtime) *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := cmd.GetKubeconfigPathSilently()

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

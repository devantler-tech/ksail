package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	cmdhelpers "github.com/devantler-tech/ksail/v5/pkg/cmd"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewExplainCmd creates the workload explain command.
func NewExplainCmd() *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := cmdhelpers.GetKubeconfigPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the explain command directly
	client := kubectl.NewClient(ioStreams)
	explainCmd := client.CreateExplainCommand(kubeconfigPath)

	return explainCmd
}

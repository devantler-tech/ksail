package workload

import (
	"os"

	"github.com/devantler-tech/ksail-go/pkg/client/kubectl"
	cmdhelpers "github.com/devantler-tech/ksail-go/pkg/cmd"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewDescribeCmd creates the workload describe command.
// The runtime parameter is kept for consistency with other workload command constructors,
// though it's currently unused as this command wraps kubectl directly.
func NewDescribeCmd(_ *runtime.Runtime) *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := cmdhelpers.GetKubeconfigPathSilently()

	// Create IO streams for kubectl
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the describe command directly
	client := kubectl.NewClient(ioStreams)
	describeCmd := client.CreateDescribeCommand(kubeconfigPath)

	return describeCmd
}

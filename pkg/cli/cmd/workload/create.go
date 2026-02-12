package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/flux"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewCreateCmd creates the workload create command.
// The runtime parameter is kept for consistency with other workload command constructors,
// though it's currently unused as this command wraps kubectl and flux directly.
func NewCreateCmd(_ *di.Runtime) *cobra.Command {
	// Try to load config silently to get kubeconfig path
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()

	// Create IO streams for kubectl and flux
	ioStreams := genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	// Create kubectl client and get the create command directly
	kubectlClient := kubectl.NewClient(ioStreams)
	createCmd := kubectlClient.CreateCreateCommand(kubeconfigPath)

	// Create flux client and add flux create sub-commands
	fluxClient := flux.NewClient(ioStreams, kubeconfigPath)
	fluxCreateCmd := fluxClient.CreateCreateCommand(kubeconfigPath)

	// Add all flux create sub-commands to the main create command
	for _, subCmd := range fluxCreateCmd.Commands() {
		createCmd.AddCommand(subCmd)
	}

	// Add permission annotation
	if createCmd.Annotations == nil {
		createCmd.Annotations = make(map[string]string)
	}

	createCmd.Annotations[annotations.AnnotationPermission] = "write"

	return createCmd
}

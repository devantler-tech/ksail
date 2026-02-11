package workload

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// kubectlCommandCreator is a function that creates a kubectl command given a client and kubeconfig path.
type kubectlCommandCreator func(client *kubectl.Client, kubeconfigPath string) *cobra.Command

// newKubectlCommand creates a kubectl wrapper command using the provided command creator.
// This reduces duplication across all kubectl-based workload commands.
func newKubectlCommand(creator kubectlCommandCreator) *cobra.Command {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	return creator(client, kubeconfigPath)
}

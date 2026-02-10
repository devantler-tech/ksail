package workload

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers/iostreams"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// kubectlCommandCreator is a function that creates a kubectl command given a client and kubeconfig path.
type kubectlCommandCreator func(client *kubectl.Client, kubeconfigPath string) *cobra.Command

// newKubectlCommand creates a kubectl wrapper command using the provided command creator.
// This reduces duplication across all kubectl-based workload commands.
func newKubectlCommand(creator kubectlCommandCreator) *cobra.Command {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	client := kubectl.NewClient(iostreams.NewStandardIOStreams())

	return creator(client, kubeconfigPath)
}

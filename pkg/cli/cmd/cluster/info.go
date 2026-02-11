package cluster

import (
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// NewInfoCmd creates the cluster info command.
func NewInfoCmd(_ *di.Runtime) *cobra.Command {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently()
	client := kubectl.NewClient(genericiooptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	})

	return client.CreateClusterInfoCommand(kubeconfigPath)
}

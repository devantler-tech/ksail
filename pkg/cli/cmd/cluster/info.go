package cluster

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewInfoCmd creates the cluster info command.
func NewInfoCmd(_ *di.Runtime) *cobra.Command {
	kubeconfigPath := helpers.GetKubeconfigPathSilently()
	client := kubectl.NewClient(helpers.NewStandardIOStreams())

	return client.CreateClusterInfoCommand(kubeconfigPath)
}

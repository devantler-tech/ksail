package gen

import (
	"github.com/devantler-tech/ksail/pkg/client/kubectl"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	"github.com/spf13/cobra"
)

// NewDeploymentCmd creates the gen deployment command.
func NewDeploymentCmd(rt *runtime.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateDeploymentCmd)
}

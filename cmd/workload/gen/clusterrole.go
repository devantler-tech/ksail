package gen

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewClusterRoleCmd creates the gen clusterrole command.
func NewClusterRoleCmd(rt *runtime.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateClusterRoleCmd)
}

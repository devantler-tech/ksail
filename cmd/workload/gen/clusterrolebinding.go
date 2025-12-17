package gen

import (
	"github.com/devantler-tech/ksail/pkg/client/kubectl"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	"github.com/spf13/cobra"
)

// NewClusterRoleBindingCmd creates the gen clusterrolebinding command.
func NewClusterRoleBindingCmd(rt *runtime.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateClusterRoleBindingCmd)
}

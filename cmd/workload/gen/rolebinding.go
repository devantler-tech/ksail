package gen

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewRoleBindingCmd creates the gen rolebinding command.
func NewRoleBindingCmd(rt *runtime.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateRoleBindingCmd)
}

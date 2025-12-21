package gen

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewServiceCmd creates the gen service command.
func NewServiceCmd(rt *runtime.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateServiceCmd)
}

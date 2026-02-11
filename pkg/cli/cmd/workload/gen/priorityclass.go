package gen

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewPriorityClassCmd creates the gen priorityclass command.
func NewPriorityClassCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreatePriorityClassCmd)
}

package gen

import (
	"github.com/devantler-tech/ksail/pkg/client/kubectl"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	"github.com/spf13/cobra"
)

// NewPodDisruptionBudgetCmd creates the gen poddisruptionbudget command.
func NewPodDisruptionBudgetCmd(rt *runtime.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreatePodDisruptionBudgetCmd)
}

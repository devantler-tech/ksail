package gen

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewQuotaCmd creates the gen quota command.
func NewQuotaCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateQuotaCmd)
}

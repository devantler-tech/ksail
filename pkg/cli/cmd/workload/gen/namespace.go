package gen

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewNamespaceCmd creates the gen namespace command.
func NewNamespaceCmd(rt *di.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateNamespaceCmd)
}

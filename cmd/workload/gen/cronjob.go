package gen

import (
	"github.com/devantler-tech/ksail/v5/pkg/client/kubectl"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewCronJobCmd creates the gen cronjob command.
func NewCronJobCmd(rt *runtime.Runtime) *cobra.Command {
	return createGenCmd(rt, (*kubectl.Client).CreateCronJobCmd)
}

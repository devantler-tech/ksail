package components

import (
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// InstallCNI delegates to the create package for CNI installation.
// Returns true if a CNI was installed, false if using default/none.
//
// Deprecated: Use create.InstallCNI directly. This wrapper exists for backward compatibility.
func InstallCNI(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
	firstActivityShown *bool,
) (bool, error) {
	return create.InstallCNI(cmd, clusterCfg, tmr, firstActivityShown)
}

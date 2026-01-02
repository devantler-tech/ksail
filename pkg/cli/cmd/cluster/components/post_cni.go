package components

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// InstallPostCNIComponents delegates to the setup package for post-CNI component installation.
//
// Deprecated: Use setup.InstallPostCNIComponents directly. This wrapper exists for backward compatibility.
func InstallPostCNIComponents(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *setup.InstallerFactories,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	err := setup.InstallPostCNIComponents(cmd, clusterCfg, factories, tmr, firstActivityShown)
	if err != nil {
		return fmt.Errorf("failed to install post-CNI components: %w", err)
	}

	return nil
}

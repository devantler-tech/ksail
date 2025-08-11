package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/cmd/shared"
	"github.com/devantler-tech/ksail/internal/util"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)


var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a new project",
	Long: `Scaffold a new Kubernetes project in the specified directory.

  Includes:
  - 'ksail.yaml' configuration file for configuring KSail
  - 'kind.yaml'|'k3d.yaml'|'talos/*' configuration file(s) for configuring the distribution
  - '.sops.yaml' for managing secrets with SOPS (optional)
  - 'k8s/kustomization.yaml' as an entry point for Kustomize
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleInit()
	},
}

// --- internals ---

// handleInit handles the init command.
func handleInit() error {
	ksailConfig := ksailcluster.NewCluster()
	setInitialValuesFromInput(ksailConfig)
	return scaffold(ksailConfig)
}

// scaffold generates initial project files according to the provided configuration.
func scaffold(ksailConfig *ksailcluster.Cluster) error {
	scaffolder := util.NewScaffolder(*ksailConfig)
	fmt.Println("📝 Scaffolding new project")
	if err := scaffolder.Scaffold(shared.Output, shared.Force); err != nil {
		return err
	}
	fmt.Println("✔ project scaffolded")
	return nil
}

// setInitialValuesFromInput mutates ksailConfig with CLI-provided values.
func setInitialValuesFromInput(ksailConfig *ksailcluster.Cluster) {
  if shared.Name != "" {
	  ksailConfig.Metadata.Name = shared.Name
  }
  if shared.Distribution != "" {
	  ksailConfig.Spec.Distribution = shared.Distribution
  }
  if shared.ReconciliationTool != "" {
	  ksailConfig.Spec.ReconciliationTool = shared.ReconciliationTool
  }
  if shared.SourceDirectory != "" {
	  ksailConfig.Spec.SourceDirectory = shared.SourceDirectory
  }
}

// init initializes the init command.
func init() {
	rootCmd.AddCommand(initCmd)
	shared.AddNameFlag(initCmd)
	shared.AddDistributionFlag(initCmd)
	shared.AddReconciliationToolFlag(initCmd)
	shared.AddSourceDirectoryFlag(initCmd)
	shared.AddForceFlag(initCmd)
}

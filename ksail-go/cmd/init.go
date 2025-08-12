package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/cmd/helpers"
	"github.com/devantler-tech/ksail/cmd/inputs"
	"github.com/devantler-tech/ksail/internal/utils"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

// initCmd represents the init command
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
	ksailConfig.Metadata.Name = helpers.InputOrFallback(inputs.Name, ksailConfig.Metadata.Name)
	ksailConfig.Spec.Distribution = helpers.InputOrFallback(inputs.Distribution, ksailConfig.Spec.Distribution)
	ksailConfig.Spec.ReconciliationTool = helpers.InputOrFallback(inputs.ReconciliationTool, ksailConfig.Spec.ReconciliationTool)
	ksailConfig.Spec.SourceDirectory = helpers.InputOrFallback(inputs.SourceDirectory, ksailConfig.Spec.SourceDirectory)
	return scaffold(ksailConfig)
}

// scaffold generates initial project files according to the provided configuration.
func scaffold(ksailConfig *ksailcluster.Cluster) error {
	scaffolder := utils.NewScaffolder(*ksailConfig)
	fmt.Println("📝 Scaffolding new project")
	if err := scaffolder.Scaffold(inputs.Output, inputs.Force); err != nil {
		return err
	}
	fmt.Println("✔ project scaffolded")
	return nil
}

// init initializes the init command.
func init() {
	rootCmd.AddCommand(initCmd)
	inputs.AddNameFlag(initCmd)
	inputs.AddDistributionFlag(initCmd)
	inputs.AddReconciliationToolFlag(initCmd)
	inputs.AddSourceDirectoryFlag(initCmd)
	inputs.AddForceFlag(initCmd, "overwrite files")
}

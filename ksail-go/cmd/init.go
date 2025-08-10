package cmd

import (
	"fmt"

	"devantler.tech/ksail/internal/util"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

var (
	initName               string                          = "ksail-default"
	initDistribution       ksailcluster.Distribution       = ksailcluster.DistributionKind
	initReconciliationTool ksailcluster.ReconciliationTool = ksailcluster.ReconciliationToolKubectl
	initOutput             string                          = "./"
	initSrcDir             string                          = "k8s"
	initForce              bool                            = false
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
		return HandleInit()
	},
}

// HandleInit handles the init command.
func HandleInit() error {
	ksailConfig := ksailcluster.NewCluster()
	SetInitialValuesFromInput(ksailConfig)
	return Scaffold(ksailConfig)
}

// Scaffold generates initial project files according to the provided configuration.
func Scaffold(ksailConfig *ksailcluster.Cluster) error {
	scaffolder := util.NewScaffolder(*ksailConfig)
	fmt.Println("📝 Scaffolding new project...")
	if err := scaffolder.Scaffold(initOutput, initForce); err != nil {
		return err
	}
	fmt.Println("✔ project scaffolded")
	return nil
}

// TODO: Move SetInitialValuesFromInput to a more fitting file
// SetInitialValuesFromInput mutates clusterObj with CLI-provided values.
func SetInitialValuesFromInput(ksailConfig *ksailcluster.Cluster) {
	ksailConfig.Metadata.Name = initName
	ksailConfig.Spec.Distribution = initDistribution
	ksailConfig.Spec.ReconciliationTool = initReconciliationTool
	ksailConfig.Spec.SourceDirectory = initSrcDir
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVarP(&initOutput, "output", "o", "./", "output directory")
	initCmd.Flags().StringVarP(&initName, "name", "n", "ksail-default", "name of cluster")
	initCmd.Flags().VarP(&initDistribution, "distribution", "d", "distribution to use")
	initCmd.Flags().VarP(&initReconciliationTool, "reconciliation-tool", "r", "reconciliation tool to use")
	initCmd.Flags().StringVarP(&initSrcDir, "source-directory", "", "k8s", "manifests source directory")
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "overwrite files")
}

package cmd

import (
	"fmt"

	"devantler.tech/ksail/internal/util"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

var (
	name         string               = "ksail-default"
	distribution ksailcluster.Distribution = ksailcluster.DistributionKind
	output       string               = "./"
	srcDir       string               = "k8s"
	force        bool                 = false
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
		ksailConfig := ksailcluster.NewCluster()
		SetInitialValuesFromInput(ksailConfig, name, distribution, srcDir)
	return Scaffold(*ksailConfig, output, force)
	},
}

// Scaffold generates initial project files according to the provided configuration.
func Scaffold(ksailConfig ksailcluster.Cluster, output string, force bool) error {
	scaffolder := util.NewScaffolder(ksailConfig)
	fmt.Println("📝 Scaffolding new project...")
	if err := scaffolder.Scaffold(output, force); err != nil {
		return err
	}
	fmt.Println("✔ project scaffolded")
	return nil
}

// TODO: Move SetInitialValuesFromInput to a more fitting file
// SetInitialValuesFromInput mutates clusterObj with CLI-provided values.
func SetInitialValuesFromInput(clusterObj *ksailcluster.Cluster, name string, distribution ksailcluster.Distribution, srcDir string) {
	clusterObj.Metadata.Name = name
	clusterObj.Spec.Distribution = distribution
	clusterObj.Spec.SourceDirectory = srcDir
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVarP(&output, "output", "o", "./", "Output directory")
	initCmd.Flags().StringVarP(&name, "name", "n", "ksail-default", "Name of the KSail cluster")
	initCmd.Flags().VarP(&distribution, "distribution", "d", "Kubernetes distribution to use (kind, k3d, talos-in-docker)")
	initCmd.Flags().StringVarP(&srcDir, "source-directory", "", "k8s", "Relative path to the source directory")
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing files if present")
}

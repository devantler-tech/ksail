package shared

import (
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

var (
	Output             string                          = "./"
	Name               string                          = "ksail-default"
	Distribution       ksailcluster.Distribution       = ksailcluster.DistributionKind
	ReconciliationTool ksailcluster.ReconciliationTool = ksailcluster.ReconciliationToolKubectl
	SourceDirectory    string                          = "k8s"
	Force              bool                            = false
	All                bool                            = false
)

// AddOutputFlag adds the --output flag to the given command.
func AddOutputFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&Output, "output", "o", "./", "output directory")
}

// AddNameFlag adds the --name flag to the given command.
func AddNameFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&Name, "name", "n", "", "name of cluster")
}

// AddDistributionFlag adds the --distribution flag to the given command.
func AddDistributionFlag(cmd *cobra.Command) {
	cmd.Flags().VarP(&Distribution, "distribution", "d", "distribution to use")
}

// AddReconciliationToolFlag adds the --reconciliation-tool flag to the given command.
func AddReconciliationToolFlag(cmd *cobra.Command) {
	cmd.Flags().VarP(&ReconciliationTool, "reconciliation-tool", "r", "reconciliation tool to use")
}

// AddSourceDirectoryFlag adds the --source-directory flag to the given command.
func AddSourceDirectoryFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&SourceDirectory, "source-directory", "s", "k8s", "manifests source directory")
}

// AddForceFlag adds the --force flag to the given command.
func AddForceFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&Force, "force", "f", false, "force operation")
}

// AddAllFlag adds the --all flag to the given command.
func AddAllFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&All, "all", "a", false, "include all resources")
}

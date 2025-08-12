package inputs

import (
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

var (
	Name               string
	ContainerEngine    ksailcluster.ContainerEngine
	Distribution       ksailcluster.Distribution
	ReconciliationTool ksailcluster.ReconciliationTool
	SourceDirectory    string
	// cli flag only
	Output string
	Force  bool
	All    bool
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
	cmd.Flags().StringVarP(&SourceDirectory, "source-directory", "s", "", "manifests source directory")
}

// AddForceFlag adds the --force flag to the given command.
func AddForceFlag(cmd *cobra.Command, description ...string) {
	desc := "force operation"
	if len(description) > 0 {
		desc = description[0]
	}
	cmd.Flags().BoolVarP(&Force, "force", "f", false, desc)
}

// AddAllFlag adds the --all flag to the given command.
func AddAllFlag(cmd *cobra.Command, description ...string) {
	desc := "include all resources"
	if len(description) > 0 {
		desc = description[0]
	}
	cmd.Flags().BoolVarP(&All, "all", "a", false, desc)
}

// AddContainerEngineFlag adds the --container-engine flag to the given command.
func AddContainerEngineFlag(cmd *cobra.Command) {
	cmd.Flags().VarP(&ContainerEngine, "container-engine", "c", "container engine to use")
}

package cmd

import (
	"fmt"

	"devantler.tech/ksail/cmd/helpers"
	"devantler.tech/ksail/cmd/shared"
	factory "devantler.tech/ksail/internal"
	"devantler.tech/ksail/internal/loader"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

// upCmd represents the up command
var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Provision a new Kubernetes cluster",
	Long: `Provision a new Kubernetes cluster using the 'ksail.yaml' configuration.

  If not found in the current directory, it will search the parent directories, and use the first one it finds.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleUp()
	},
}

// --- internals ---

// handleUp handles the up command.
func handleUp() error {
	ksailConfig, err := loader.NewKSailConfigLoader().Load()
	if err != nil {
		return err
	}
	if err := provision(&ksailConfig); err != nil {
		return err
	}
	return nil
}

// provision provisions a cluster based on the provided configuration.
func provision(ksailConfig *ksailcluster.Cluster) error {
	name := helpers.Name(ksailConfig, shared.Name)
	distribution := helpers.Distribution(ksailConfig, shared.Distribution)
	reconciliationTool := helpers.ReconciliationTool(ksailConfig, shared.ReconciliationTool)

	provisioner, err := factory.Provisioner(distribution, ksailConfig)
	if err != nil {
		return err
	}

	reconciliationToolBootstrapper, err := factory.ReconciliationTool(reconciliationTool, ksailConfig)
	if err != nil {
		return err
	}

	fmt.Printf("🚀 Provisioning '%s' with '%s'...\n", name, distribution)
	if shared.Force {
		exists, err := provisioner.Exists(name)
		if err != nil {
			return err
		}
		if exists {
			fmt.Printf("► deleting existing cluster '%s'\n", name)
			if err := provisioner.Delete(name); err != nil {
				return err
			}
		}
	}
	fmt.Printf("► creating cluster '%s'\n", name)
	if err := provisioner.Create(name); err != nil {
		return err
	}

	fmt.Printf("⚙️ Bootstrapping '%s' to '%s' cluster...\n", reconciliationTool, name)
	fmt.Printf("► installing '%s'\n", reconciliationTool)
	_ = reconciliationToolBootstrapper.Install()

	return nil
}

func init() {
	rootCmd.AddCommand(upCmd)
	shared.AddNameFlag(upCmd)
	shared.AddDistributionFlag(upCmd)
	shared.AddReconciliationToolFlag(upCmd)
	shared.AddForceFlag(upCmd)
}

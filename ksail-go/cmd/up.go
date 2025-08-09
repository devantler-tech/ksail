package cmd

import (
	"fmt"
	"os"

	"devantler.tech/ksail/internal/ui/notify"
	"devantler.tech/ksail/internal/util"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	kindProvisioner "devantler.tech/ksail/pkg/provisioner/cluster/kind"
	"github.com/spf13/cobra"
)

var (
	upForce bool
)

// upCmd represents the up command
var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Provision a new Kubernetes cluster",
	Long: `Provision a new Kubernetes cluster using the 'ksail.yaml' configuration.

  If not found in the current directory, it will search the parent directories, and use the first one it finds.`,
	Run: func(cmd *cobra.Command, args []string) {
		ksailConfigLoader := util.NewKSailConfigLoader()
		ksailConfig, err := ksailConfigLoader.LoadKSailConfig()
		if err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
		if err := Provision(ksailConfig); err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
	},
}

// Provision provisions a cluster based on the provided configuration.
func Provision(ksailConfig *ksailcluster.Cluster) error {
	kindConfigLoader := util.NewKindConfigLoader()
	kindConfig, err := kindConfigLoader.LoadKindConfig()
	if err != nil {
		notify.Errorf("%s", err)
		os.Exit(1)
	}
	kindProvisioner := kindProvisioner.NewKindClusterProvisioner(ksailConfig, kindConfig)

  fmt.Printf("🚀 Provisioning '%s' with '%s'...\n", ksailConfig.Metadata.Name, ksailConfig.Spec.Distribution)
	if upForce {
		fmt.Printf("► deleting existing cluster '%s'...\n", kindConfig.Name)
		exists, err := kindProvisioner.Exists()
		if err != nil {
			return err
		}
		if exists {
			if err := kindProvisioner.Delete(); err != nil {
				return err
			}
		}
	}
  fmt.Printf("► creating cluster '%s'...\n", kindConfig.Name)
	if err := kindProvisioner.Create(); err != nil {
		return err
	}
	return nil
}

func init() {
	rootCmd.AddCommand(upCmd)
	upCmd.Flags().BoolVarP(&upForce, "force", "f", false, "If set, delete any existing cluster before creating a new one")
}

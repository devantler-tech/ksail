package cmd

import (
	"fmt"
	"os"

	"devantler.tech/ksail/internal/util"
	color "devantler.tech/ksail/internal/util/fmt"
	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	kindProvisioner "devantler.tech/ksail/pkg/provisioner/cluster/kind"
	"github.com/spf13/cobra"
)

// upCmd represents the up command
var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Provision a new Kubernetes cluster",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		ksailConfigLoader := util.NewKSailConfigLoader()
		ksailConfig := ksailConfigLoader.LoadKSailConfig()
		Provision(ksailConfig)
	},
}

func Provision(ksailConfig *cluster.Cluster) {
	kindProvisioner := kindProvisioner.NewKindClusterProvisioner(ksailConfig)
	fmt.Printf("🚀 Provisioning '%s' with '%s'...\n", ksailConfig.Metadata.Name, ksailConfig.Spec.Distribution)
	err := kindProvisioner.Create(ksailConfig.Metadata.Name, "kind.yaml")
	if err != nil {
		color.PrintError("%s", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(upCmd)
}

/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"

	"devantler.tech/ksail/internal/ui/notify"
	"devantler.tech/ksail/internal/util"
	kindProvisioner "devantler.tech/ksail/pkg/provisioner/cluster/kind"
	"github.com/spf13/cobra"
)

var (
	downName string
)

// downCmd represents the down command
var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Destroy an existing Kubernetes cluster",
	Long:  "Destroy an existing Kubernetes cluster specified by --name or by the loaded kind config.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := Down(downName); err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
	},
}

// Down tears down a cluster using the provided name or the loaded kind config name.
func Down(nameFlag string) error {
	ksailConfigLoader := util.NewKSailConfigLoader()
	ksailConfig, err := ksailConfigLoader.LoadKSailConfig()
	if err != nil {
		return err
	}

	kindConfigLoader := util.NewKindConfigLoader()
	kindConfig, err := kindConfigLoader.LoadKindConfig()
	if err != nil {
		notify.Warnf("%s", err)
	}

	// Determine target name
	targetName := nameFlag
	if targetName == "" {
		// Prefer the kind config's name if set; fall back to ksail config name
		if kindConfig != nil && kindConfig.Name != "" {
			targetName = kindConfig.Name
		} else {
			targetName = ksailConfig.Metadata.Name
		}
	}

	// Provisioner and delete
	prov := kindProvisioner.NewKindClusterProvisioner(ksailConfig, kindConfig)
	fmt.Printf("🔥 Destroying '%s'...\n", targetName)
	exists, err := prov.ExistsByName(targetName)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("► No cluster named '%s' found. Nothing to do.\n", targetName)
		return nil
	}
	if err := prov.DeleteByName(targetName); err != nil {
		return err
	}
	fmt.Println("► Cluster deleted.")
	return nil
}

func init() {
	rootCmd.AddCommand(downCmd)
	downCmd.Flags().StringVarP(&downName, "name", "n", "", "Name of the cluster to destroy (defaults to kind config name)")
}

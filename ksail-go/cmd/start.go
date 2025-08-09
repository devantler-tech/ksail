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

var startName string

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start an existing Kubernetes cluster",
	Long:  "Start an existing Kubernetes cluster specified by --name or by the loaded kind config.",
	Run: func(cmd *cobra.Command, args []string) {
		ksailConfig, err := util.NewKSailConfigLoader().LoadKSailConfig()
		if err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
		kindConfig, _ := util.NewKindConfigLoader().LoadKindConfig()
		target := startName
		if target == "" {
			if kindConfig != nil && kindConfig.Name != "" {
				target = kindConfig.Name
			} else {
				target = ksailConfig.Metadata.Name
			}
		}
		prov := kindProvisioner.NewKindClusterProvisioner(ksailConfig, kindConfig)
		fmt.Printf("▶️ Starting '%s'...\n", target)
		exists, err := prov.ExistsByName(target)
		if err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
		if !exists {
			fmt.Printf("► No cluster named '%s' found. Nothing to do.\n", target)
			return
		}
		if err := prov.StartByName(target); err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
		fmt.Println("► Cluster started.")
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().StringVarP(&startName, "name", "n", "", "Name of the cluster to start (defaults to kind config name)")
}

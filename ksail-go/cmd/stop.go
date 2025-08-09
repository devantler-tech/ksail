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

var stopName string

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop an existing Kubernetes cluster",
	Long:  "Stop an existing Kubernetes cluster specified by --name or by the loaded kind config.",
	Run: func(cmd *cobra.Command, args []string) {
		ksailConfig, err := util.NewKSailConfigLoader().LoadKSailConfig()
		if err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
		kindConfig, _ := util.NewKindConfigLoader().LoadKindConfig()
		target := stopName
		if target == "" {
			if kindConfig != nil && kindConfig.Name != "" {
				target = kindConfig.Name
			} else {
				target = ksailConfig.Metadata.Name
			}
		}
		prov := kindProvisioner.NewKindClusterProvisioner(ksailConfig, kindConfig)
		fmt.Printf("⏹️ Stopping '%s'...\n", target)
		exists, err := prov.ExistsByName(target)
		if err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
		if !exists {
			fmt.Printf("► No cluster named '%s' found. Nothing to do.\n", target)
			return
		}
		if err := prov.StopByName(target); err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
		fmt.Println("► Cluster stopped.")
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
	stopCmd.Flags().StringVarP(&stopName, "name", "n", "", "Name of the cluster to stop (defaults to kind config name)")
}

/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"

	"devantler.tech/ksail/internal/ui/notify"
	"devantler.tech/ksail/internal/loader"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	clusterprov "devantler.tech/ksail/pkg/provisioner/cluster"
	confv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var (
	stopName string
	stopDistribution ksailcluster.Distribution
)

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop an existing Kubernetes cluster",
	Long:  "Stop an existing Kubernetes cluster specified by --name or by the loaded kind config.",
	Run: func(cmd *cobra.Command, args []string) {
	ksailAny, err := loader.NewKSailConfigLoader(nil).Load()
		if err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
	ksailConfig := ksailAny.(*ksailcluster.Cluster)
		// Choose distribution
		dist := ksailConfig.Spec.Distribution
		if stopDistribution != "" {
			dist = stopDistribution
		}

		switch dist {
		case ksailcluster.DistributionKind:
			kindAny, _ := loader.NewKindConfigLoader(nil).Load()
			kindConfig, _ := kindAny.(*v1alpha4.Cluster)
			target := stopName
			if target == "" {
				if kindConfig != nil && kindConfig.Name != "" {
					target = kindConfig.Name
				} else {
					target = ksailConfig.Metadata.Name
				}
			}
			prov := clusterprov.NewKindClusterProvisioner(ksailConfig, kindConfig)
			fmt.Printf("⏹️ Stopping '%s'...\n", target)
			exists, err := prov.Exists(target)
			if err != nil {
				notify.Errorf("%s", err)
				os.Exit(1)
			}
			if !exists {
				fmt.Printf("► No cluster named '%s' found. Nothing to do.\n", target)
				return
			}
			if err := prov.Stop(target); err != nil {
				notify.Errorf("%s", err)
				os.Exit(1)
			}
			fmt.Println("► Cluster stopped.")
		case ksailcluster.DistributionK3d:
			k3dAny, _ := loader.NewK3dConfigLoader(nil).Load()
			k3dCfg, _ := k3dAny.(*confv1alpha5.SimpleConfig)
			target := stopName
			if target == "" {
				if k3dCfg != nil && k3dCfg.Name != "" {
					target = k3dCfg.Name
				} else {
					target = ksailConfig.Metadata.Name
				}
			}
			prov := clusterprov.NewK3dClusterProvisioner(ksailConfig, k3dCfg)
			fmt.Printf("⏹️ Stopping '%s'...\n", target)
			exists, err := prov.Exists(target)
			if err != nil {
				notify.Errorf("%s", err)
				os.Exit(1)
			}
			if !exists {
				fmt.Printf("► No cluster named '%s' found. Nothing to do.\n", target)
				return
			}
			if err := prov.Stop(target); err != nil {
				notify.Errorf("%s", err)
				os.Exit(1)
			}
			fmt.Println("► Cluster stopped.")
		default:
			notify.Errorf("unsupported distribution: %s", dist)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
	stopCmd.Flags().StringVarP(&stopName, "name", "n", "", "Name of the cluster to stop (defaults to kind config name)")
	stopCmd.Flags().VarP(&stopDistribution, "distribution", "d", "Override the distribution: Kind|K3d|Tind")
}

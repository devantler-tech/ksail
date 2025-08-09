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
	downName string
	downDistribution ksailcluster.Distribution
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
	ksailConfigAny, err := loader.NewKSailConfigLoader(nil).Load()
	if err != nil {
		return err
	}
	ksailConfig := ksailConfigAny.(*ksailcluster.Cluster)

	// Choose distribution
	dist := ksailConfig.Spec.Distribution
	if downDistribution != "" {
		dist = downDistribution
	}

	// Determine target name
	targetName := nameFlag
	switch dist {
	case ksailcluster.DistributionKind:
	kindAny, _ := loader.NewKindConfigLoader(nil).Load()
	kindConfig, _ := kindAny.(*v1alpha4.Cluster)
		if targetName == "" {
			if kindConfig != nil && kindConfig.Name != "" {
				targetName = kindConfig.Name
			} else {
				targetName = ksailConfig.Metadata.Name
			}
		}
	prov := clusterprov.NewKindClusterProvisioner(ksailConfig, kindConfig)
		fmt.Printf("🔥 Destroying '%s'...\n", targetName)
	exists, err := prov.Exists(targetName)
		if err != nil {
			return err
		}
		if !exists {
			fmt.Printf("► No cluster named '%s' found. Nothing to do.\n", targetName)
			return nil
		}
	if err := prov.Delete(targetName); err != nil {
			return err
		}
		fmt.Println("► Cluster deleted.")
		return nil
	case ksailcluster.DistributionK3d:
	k3dAny, _ := loader.NewK3dConfigLoader(nil).Load()
	k3dCfg, _ := k3dAny.(*confv1alpha5.SimpleConfig)
		if targetName == "" {
			if k3dCfg != nil && k3dCfg.Name != "" {
				targetName = k3dCfg.Name
			} else {
				targetName = ksailConfig.Metadata.Name
			}
		}
	prov := clusterprov.NewK3dClusterProvisioner(ksailConfig, k3dCfg)
		fmt.Printf("🔥 Destroying '%s'...\n", targetName)
	exists, err := prov.Exists(targetName)
		if err != nil {
			return err
		}
		if !exists {
			fmt.Printf("► No cluster named '%s' found. Nothing to do.\n", targetName)
			return nil
		}
	if err := prov.Delete(targetName); err != nil {
			return err
		}
		fmt.Println("► Cluster deleted.")
		return nil
	default:
		return fmt.Errorf("unsupported distribution: %s", dist)
	}
}

func init() {
	rootCmd.AddCommand(downCmd)
	downCmd.Flags().StringVarP(&downName, "name", "n", "", "Name of the cluster to destroy (defaults to kind config name)")
	downCmd.Flags().VarP(&downDistribution, "distribution", "d", "Override the distribution: Kind|K3d|Tind")
}

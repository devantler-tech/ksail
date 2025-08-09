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
	upForce bool
	upDistribution ksailcluster.Distribution
)

// upCmd represents the up command
var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Provision a new Kubernetes cluster",
	Long: `Provision a new Kubernetes cluster using the 'ksail.yaml' configuration.

  If not found in the current directory, it will search the parent directories, and use the first one it finds.`,
	Run: func(cmd *cobra.Command, args []string) {
	ksailAny, err := loader.NewKSailConfigLoader(nil).Load()
		if err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
	ksailConfig := ksailAny.(*ksailcluster.Cluster)
		if err := Provision(ksailConfig); err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
	},
}

// Provision provisions a cluster based on the provided configuration.
func Provision(ksailConfig *ksailcluster.Cluster) error {
	// Choose distribution: flag overrides ksail.yaml if set
	dist := ksailConfig.Spec.Distribution
	if upDistribution != "" {
		dist = upDistribution
	}

	// Use a generic loader to ensure "⏳ Loading ..." prints happen before provisioning
	var loaderIface loader.ConfigLoader
	switch dist {
	case ksailcluster.DistributionKind:
		loaderIface = loader.NewKindConfigLoader(nil)
	case ksailcluster.DistributionK3d:
		loaderIface = loader.NewK3dConfigLoader(nil)
	default:
		return fmt.Errorf("unsupported distribution: %s", dist)
	}

	cfg, err := loaderIface.Load()
	if err != nil {
		notify.Errorf("%s", err)
		os.Exit(1)
	}

	fmt.Printf("🚀 Provisioning '%s' with '%s'...\n", ksailConfig.Metadata.Name, dist)

	switch dist {
	case ksailcluster.DistributionKind:
		kindCfg, _ := cfg.(*v1alpha4.Cluster)
	prov := clusterprov.NewKindClusterProvisioner(ksailConfig, kindCfg)
		if upForce {
			fmt.Printf("► deleting existing cluster '%s'...\n", ksailConfig.Metadata.Name)
			exists, err := prov.Exists("")
			if err != nil {
				return err
			}
			if exists {
				if err := prov.Delete(""); err != nil {
					return err
				}
			}
		}
		fmt.Printf("► creating cluster '%s'...\n", ksailConfig.Metadata.Name)
	return prov.Create("")
	case ksailcluster.DistributionK3d:
		k3dCfg, _ := cfg.(*confv1alpha5.SimpleConfig)
	prov := clusterprov.NewK3dClusterProvisioner(ksailConfig, k3dCfg)
		if upForce {
			fmt.Printf("► deleting existing cluster '%s'...\n", ksailConfig.Metadata.Name)
			exists, err := prov.Exists("")
			if err != nil {
				return err
			}
			if exists {
				if err := prov.Delete(""); err != nil {
					return err
				}
			}
		}
		fmt.Printf("► creating cluster '%s'...\n", ksailConfig.Metadata.Name)
	return prov.Create("")
	default:
		return fmt.Errorf("unsupported distribution: %s", dist)
	}
}

func init() {
	rootCmd.AddCommand(upCmd)
	upCmd.Flags().BoolVarP(&upForce, "force", "f", false, "If set, delete any existing cluster before creating a new one")
	upCmd.Flags().VarP(&upDistribution, "distribution", "d", "Override the distribution: Kind|K3d|Tind")
}

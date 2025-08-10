package cmd

import (
	"fmt"
	"os"

	"devantler.tech/ksail/internal/loader"
	"devantler.tech/ksail/internal/ui/notify"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	reconboot "devantler.tech/ksail/pkg/bootstrapper/reconciliation_tool"
	clusterprov "devantler.tech/ksail/pkg/provisioner/cluster"
	confv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var (
	upForce              bool
	upDistribution       ksailcluster.Distribution
	upReconciliationTool ksailcluster.ReconciliationTool
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

	var provisioner clusterprov.ClusterProvisioner
	switch dist {
	case ksailcluster.DistributionKind:
		kindCfg, _ := cfg.(*v1alpha4.Cluster)
		provisioner = clusterprov.NewKindClusterProvisioner(ksailConfig, kindCfg)
		if upForce {
			fmt.Printf("► deleting existing cluster '%s'...\n", ksailConfig.Metadata.Name)
			exists, err := provisioner.Exists("")
			if err != nil {
				return err
			}
			if exists {
				if err := provisioner.Delete(""); err != nil {
					return err
				}
			}
		}
		fmt.Printf("► creating cluster '%s'...\n", ksailConfig.Metadata.Name)
		provisioner.Create("")
	case ksailcluster.DistributionK3d:
		k3dCfg, _ := cfg.(*confv1alpha5.SimpleConfig)
		provisioner = clusterprov.NewK3dClusterProvisioner(ksailConfig, k3dCfg)
		if upForce {
			fmt.Printf("► deleting existing cluster '%s'...\n", ksailConfig.Metadata.Name)
			exists, err := provisioner.Exists("")
			if err != nil {
				return err
			}
			if exists {
				if err := provisioner.Delete(""); err != nil {
					return err
				}
			}
		}
		fmt.Printf("► creating cluster '%s'...\n", ksailConfig.Metadata.Name)
		provisioner.Create("")
	default:
		return fmt.Errorf("unsupported distribution: %s", dist)
	}

	fmt.Printf("⚙️ Bootstrapping '%s' to '%s' cluster...\n", ksailConfig.Spec.ReconciliationTool, ksailConfig.Metadata.Name)
	var reconciliationToolBootstrapper reconboot.Bootstrapper
	switch ksailConfig.Spec.ReconciliationTool {
	case ksailcluster.ReconciliationToolKubectl:
		// Bootstrap with kubectl
	case ksailcluster.ReconciliationToolFlux:
		reconciliationToolBootstrapper = reconboot.NewFluxOperatorBootstrapper(
			ksailConfig.Spec.Connection.Kubeconfig,
			ksailConfig.Spec.Connection.Context,
		)
		reconciliationToolBootstrapper.Install()
	case ksailcluster.ReconciliationToolArgoCD:
		// Bootstrap with ArgoCD
	default:
		return fmt.Errorf("unsupported reconciliation tool: %s", ksailConfig.Spec.ReconciliationTool)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(upCmd)
	upCmd.Flags().BoolVarP(&upForce, "force", "f", false, "If set, delete any existing cluster before creating a new one")
	upCmd.Flags().VarP(&upDistribution, "distribution", "d", "Override the distribution: Kind|K3d|Tind")
	upCmd.Flags().VarP(&upReconciliationTool, "reconciliation-tool", "r", "Override the reconciliation tool: Kubectl|Flux|ArgoCD")
}

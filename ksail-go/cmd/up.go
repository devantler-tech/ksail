package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/cmd/helpers"
	"github.com/devantler-tech/ksail/cmd/inputs"
	factory "github.com/devantler-tech/ksail/internal/factories"
	"github.com/devantler-tech/ksail/internal/loader"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
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
	ksailConfig.Metadata.Name = helpers.InputOrFallback(ksailConfig.Metadata.Name, inputs.Name)
	ksailConfig.Spec.ContainerEngine = helpers.InputOrFallback(ksailConfig.Spec.ContainerEngine, inputs.ContainerEngine)
	containerEngineProvisioner, err := factory.ContainerEngineProvisioner(ksailConfig)
	if err != nil {
		return err
	}

  fmt.Println("📋 Checking prerequisites")
  fmt.Printf("► checking '%s' is ready\n", ksailConfig.Spec.ContainerEngine)
	ready, err := containerEngineProvisioner.CheckReady()
	if err != nil || !ready {
		return fmt.Errorf("container engine '%s' is not ready: %v", ksailConfig.Spec.ContainerEngine, err)
	}
  fmt.Printf("✔ '%s' is ready\n", ksailConfig.Spec.ContainerEngine)

	// TODO: Create local registry 'ksail-registry' with a docker provisioner

	err = provisionCluster(ksailConfig)
	if err != nil {
		return err
	}

	// TODO: Bootstrap CNI with a cni provisioner

	// TODO: Bootstrap CSI with a csi provisioner

	// TODO: Bootstrap IngressController with an ingress controller provisioner

	// TODO: Bootstrap GatewayController with a gateway controller provisioner

	// TODO: Bootstrap CertManager with a cert manager provisioner

	// TODO: Bootstrap Metrics Server with a metrics server provisioner

	err = bootstrapReconciliationTool(ksailConfig)
	if err != nil {
		return err
	}

	return nil
}

// provisionCluster provisions a cluster based on the provided configuration.
func provisionCluster(ksailConfig *ksailcluster.Cluster) error {
	fmt.Println()
	ksailConfig.Spec.Distribution = helpers.InputOrFallback(ksailConfig.Spec.Distribution, inputs.Distribution)
	ksailConfig.Spec.ContainerEngine = helpers.InputOrFallback(ksailConfig.Spec.ContainerEngine, inputs.ContainerEngine)
	provisioner, err := factory.ClusterProvisioner(ksailConfig)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Printf("🚀 Provisioning '%s'\n", ksailConfig.Metadata.Name)
	if inputs.Force {
		exists, err := provisioner.Exists(ksailConfig.Metadata.Name)
		if err != nil {
			return err
		}
		if exists {
			if err := provisioner.Delete(ksailConfig.Metadata.Name); err != nil {
				return err
			}
		}
	}
	if err := provisioner.Create(ksailConfig.Metadata.Name); err != nil {
		return err
	}
	fmt.Printf("✔ '%s' created\n", ksailConfig.Metadata.Name)
	return nil
}

func bootstrapReconciliationTool(ksailConfig *ksailcluster.Cluster) error {
	reconciliationTool := helpers.InputOrFallback(ksailConfig.Spec.ReconciliationTool, inputs.ReconciliationTool)
	reconciliationToolBootstrapper, err := factory.ReconciliationTool(reconciliationTool, ksailConfig)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("⚙️ Bootstrapping '%s' to '%s'\n", reconciliationTool, ksailConfig.Metadata.Name)
	_ = reconciliationToolBootstrapper.Install()
	fmt.Printf("✔ '%s' installed\n", reconciliationTool)
	return nil
}

func init() {
	rootCmd.AddCommand(upCmd)
	inputs.AddNameFlag(upCmd)
	inputs.AddDistributionFlag(upCmd)
	inputs.AddReconciliationToolFlag(upCmd)
	inputs.AddForceFlag(upCmd, "recreate cluster")
	inputs.AddContainerEngineFlag(upCmd)
}

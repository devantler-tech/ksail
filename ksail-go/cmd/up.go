package cmd

import (
	"fmt"
	"sync"

	"github.com/devantler-tech/ksail/cmd/inputs"
	factory "github.com/devantler-tech/ksail/internal/factories"
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
  InitServices()
	err := configValidator.Validate()
	if err != nil {
		return err
	}
	// TODO: Validate workloads
	if err := provision(); err != nil {
		return err
	}
	return nil
}

// provision provisions a cluster based on the provided configuration.
func provision() error {
	// TODO: Create local registry 'ksail-registry' with a docker provisioner

	err := provisionCluster()
	if err != nil {
		return err
	}

	// Define bootstrap functions
	bootstrapTasks := []struct {
		name string
		fn   func() error
	}{
		{"CNI", func() error {
			// TODO: Bootstrap CNI with a cni provisioner
			return nil
		}},
		{"CSI", func() error {
			// TODO: Bootstrap CSI with a csi provisioner
			return nil
		}},
		{"IngressController", func() error {
			// TODO: Bootstrap IngressController with an ingress controller provisioner
			return nil
		}},
		{"GatewayController", func() error {
			// TODO: Bootstrap GatewayController with a gateway controller provisioner
			return nil
		}},
		{"CertManager", func() error {
			// TODO: Bootstrap CertManager with a cert manager provisioner
			return nil
		}},
		{"MetricsServer", func() error {
			// TODO: Bootstrap Metrics Server with a metrics server provisioner
			return nil
		}},
		{"ReconciliationTool", bootstrapReconciliationTool},
	}

	type result struct {
		name string
		err  error
	}

	results := make([]result, len(bootstrapTasks))
	var wg sync.WaitGroup

	for i, task := range bootstrapTasks {
		wg.Add(1)
		go func(i int, taskName string, fn func() error) {
			defer wg.Done()
			results[i] = result{name: taskName, err: fn()}
		}(i, task.name, task.fn)
	}
	wg.Wait()

	// Sequential output
	for _, res := range results {
		if res.err != nil {
			return res.err
		} else {
			fmt.Printf("✔ %s bootstrapped\n", res.name)
		}
	}

	// TODO: Reconcile

	return nil
}

// provisionCluster provisions a cluster based on the provided configuration.
func provisionCluster() error {
	fmt.Println()
	fmt.Printf("🚀 Provisioning '%s'\n", ksailConfig.Metadata.Name)
	fmt.Printf("► checking '%s' is ready\n", ksailConfig.Spec.ContainerEngine)
	ready, err := containerEngineProvisioner.CheckReady()
	if err != nil || !ready {
		return fmt.Errorf("container engine '%s' is not ready: %v", ksailConfig.Spec.ContainerEngine, err)
	}
	fmt.Printf("✔ '%s' is ready\n", ksailConfig.Spec.ContainerEngine)
	fmt.Printf("► provisioning '%s'\n", ksailConfig.Metadata.Name)
	if inputs.Force {
		exists, err := clusterProvisioner.Exists(ksailConfig.Metadata.Name)
		if err != nil {
			return err
		}
		if exists {
			if err := clusterProvisioner.Delete(ksailConfig.Metadata.Name); err != nil {
				return err
			}
		}
	}
	if err := clusterProvisioner.Create(ksailConfig.Metadata.Name); err != nil {
		return err
	}
	fmt.Printf("✔ '%s' created\n", ksailConfig.Metadata.Name)
	return nil
}

func bootstrapReconciliationTool() error {
	reconciliationToolBootstrapper, err := factory.ReconciliationTool(&ksailConfig)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("⚙️ Bootstrapping '%s' to '%s'\n", ksailConfig.Spec.ReconciliationTool, ksailConfig.Metadata.Name)
	_ = reconciliationToolBootstrapper.Install()
	fmt.Printf("✔ '%s' installed\n", ksailConfig.Spec.ReconciliationTool)
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

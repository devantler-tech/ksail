/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
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

// downCmd represents the down command
var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Destroy an existing Kubernetes cluster",
	Long:  "Destroy an existing Kubernetes cluster specified by --name or by the loaded kind config.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleDown()
	},
}

// --- internals ---

// handleDown handles the down command.
func handleDown() error {
	ksailConfig, err := loader.NewKSailConfigLoader().Load()
	if err != nil {
		return err
	}
	fmt.Println()
	return teardown(&ksailConfig)
}

// teardown tears down a cluster using the provided name or the loaded kind config name.
func teardown(ksailConfig *ksailcluster.Cluster) error {
	ksailConfig.Metadata.Name = helpers.InputOrFallback(inputs.Name, ksailConfig.Metadata.Name)
	ksailConfig.Spec.Distribution = helpers.InputOrFallback(inputs.Distribution, ksailConfig.Spec.Distribution)

	provisioner, err := factory.ClusterProvisioner(ksailConfig)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("🔥 Destroying '%s'\n", ksailConfig.Metadata.Name)
	exists, err := provisioner.Exists(ksailConfig.Metadata.Name)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("✔ '%s' not found\n", ksailConfig.Metadata.Name)
		return nil
	}
	if err := provisioner.Delete(ksailConfig.Metadata.Name); err != nil {
		return err
	}
	fmt.Printf("✔ '%s' destroyed\n", ksailConfig.Metadata.Name)
	return nil
}

func init() {
	rootCmd.AddCommand(downCmd)
	inputs.AddNameFlag(downCmd)
	inputs.AddDistributionFlag(downCmd)
}

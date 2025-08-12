/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/cmd/inputs"
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
  InitServices()
	return teardown()
}

// teardown tears down a cluster using the provided name or the loaded kind config name.
func teardown() error {
	fmt.Printf("🔥 Destroying '%s'\n", ksailConfig.Metadata.Name)
	fmt.Printf("► checking '%s' is ready\n", ksailConfig.Spec.ContainerEngine)
	ready, err := containerEngineProvisioner.CheckReady()
	if err != nil || !ready {
		return fmt.Errorf("container engine '%s' is not ready: %v", ksailConfig.Spec.ContainerEngine, err)
	}
	fmt.Printf("✔ '%s' is ready\n", ksailConfig.Spec.ContainerEngine)
	fmt.Printf("► destroying '%s'\n", ksailConfig.Metadata.Name)
	exists, err := clusterProvisioner.Exists(ksailConfig.Metadata.Name)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("✔ '%s' not found\n", ksailConfig.Metadata.Name)
		return nil
	}
	if err := clusterProvisioner.Delete(ksailConfig.Metadata.Name); err != nil {
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

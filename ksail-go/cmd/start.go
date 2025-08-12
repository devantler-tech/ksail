/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/cmd/inputs"
	factory "github.com/devantler-tech/ksail/internal/factories"
	"github.com/spf13/cobra"
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start an existing Kubernetes cluster",
	Long:  "Start an existing Kubernetes cluster specified by --name or by the loaded kind config.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleStart()
	},
}

// --- internals ---

// handleStart handles the start command.
func handleStart() error {
  InitServices()
	return start()
}

func start() error {
	fmt.Println()
	provisioner, err := factory.ClusterProvisioner(&ksailConfig)
	if err != nil {
		return err
	}
	containerEngineProvisioner, err := factory.ContainerEngineProvisioner(&ksailConfig)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("▶️ Starting '%s'\n", ksailConfig.Metadata.Name)
	fmt.Printf("► checking '%s' is ready\n", ksailConfig.Spec.ContainerEngine)
	ready, err := containerEngineProvisioner.CheckReady()
	if err != nil || !ready {
		return fmt.Errorf("container engine '%s' is not ready: %v", ksailConfig.Spec.ContainerEngine, err)
	}
	fmt.Printf("✔ '%s' is ready\n", ksailConfig.Spec.ContainerEngine)
	fmt.Printf("► starting '%s'\n", ksailConfig.Metadata.Name)
	exists, err := provisioner.Exists(ksailConfig.Metadata.Name)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("✔ '%s' not found\n", ksailConfig.Metadata.Name)
		return nil
	}
	if err := provisioner.Start(ksailConfig.Metadata.Name); err != nil {
		return err
	}
	fmt.Printf("✔ '%s' started\n", ksailConfig.Metadata.Name)
	return nil
}

func init() {
	rootCmd.AddCommand(startCmd)
	inputs.AddNameFlag(startCmd)
	inputs.AddDistributionFlag(startCmd)
}

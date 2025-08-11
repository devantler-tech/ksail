/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/cmd/helpers"
	"github.com/devantler-tech/ksail/cmd/shared"
	factory "github.com/devantler-tech/ksail/internal"
	"github.com/devantler-tech/ksail/internal/loader"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
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
	ksailConfig, err := loader.NewKSailConfigLoader().Load()
	if err != nil {
		return err
	}
	return start(&ksailConfig)
}

func start(ksailConfig *ksailcluster.Cluster) error {
	name := helpers.Name(ksailConfig, shared.Name)
	distribution := helpers.Distribution(ksailConfig, shared.Distribution)

	fmt.Println()
	provisioner, err := factory.Provisioner(distribution, ksailConfig)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("▶️ Starting '%s'\n", name)
	exists, err := provisioner.Exists(name)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("✔ '%s' not found\n", name)
		return nil
	}
	if err := provisioner.Start(name); err != nil {
		return err
	}
	fmt.Printf("✔ '%s' started\n", name)
	return nil
}

func init() {
	rootCmd.AddCommand(startCmd)
	shared.AddNameFlag(startCmd)
	shared.AddDistributionFlag(startCmd)
}

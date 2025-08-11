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

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop an existing Kubernetes cluster",
	Long:  "Stop an existing Kubernetes cluster specified by --name or by the loaded kind config.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleStop()
	},
}

// -- internals ---

// handleStop handles the stop command
func handleStop() error {
	ksailConfig, err := loader.NewKSailConfigLoader().Load()
	if err != nil {
		return err
	}
	return stop(&ksailConfig)
}

func stop(ksailConfig *ksailcluster.Cluster) error {
	ksailConfig.Metadata.Name = helpers.Name(ksailConfig, inputs.Name)
	ksailConfig.Spec.Distribution = helpers.Distribution(ksailConfig, inputs.Distribution)

	fmt.Println()
	provisioner, err := factory.ClusterProvisioner(ksailConfig)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("⏹️ Stopping '%s'\n", ksailConfig.Metadata.Name)
	exists, err := provisioner.Exists(ksailConfig.Metadata.Name)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("✔ '%s' not found\n", ksailConfig.Metadata.Name)
		return nil
	}
	if err := provisioner.Stop(ksailConfig.Metadata.Name); err != nil {
		return err
	}
	fmt.Printf("✔ '%s' stopped\n", ksailConfig.Metadata.Name)
	return nil
}

func init() {
	rootCmd.AddCommand(stopCmd)
	inputs.AddNameFlag(stopCmd)
	inputs.AddDistributionFlag(stopCmd)
}

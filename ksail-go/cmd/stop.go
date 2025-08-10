/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"

	"devantler.tech/ksail/cmd/helpers"
	"devantler.tech/ksail/cmd/shared"
	factory "devantler.tech/ksail/internal"
	"devantler.tech/ksail/internal/loader"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
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
	name := helpers.Name(ksailConfig, shared.Name)
	distribution := helpers.Distribution(ksailConfig, shared.Distribution)

	provisioner, err := factory.Provisioner(distribution, ksailConfig)
	if err != nil {
		return err
	}

	exists, err := provisioner.Exists(name)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("✔ no cluster named '%s' found\n", name)
		return nil
	}
	if err := provisioner.Stop(name); err != nil {
		return err
	}
	fmt.Printf("✔ cluster named '%s' stopped\n", name)
  return nil
}

func init() {
	rootCmd.AddCommand(stopCmd)
	shared.AddNameFlag(stopCmd)
	shared.AddDistributionFlag(stopCmd)
}

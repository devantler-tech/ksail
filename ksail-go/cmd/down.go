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

var (
	downName         string
	downDistribution ksailcluster.Distribution
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
	return teardown(&ksailConfig)
}

// teardown tears down a cluster using the provided name or the loaded kind config name.
func teardown(ksailConfig *ksailcluster.Cluster) error {
	name := helpers.Name(ksailConfig, shared.Name)
	distribution := helpers.Distribution(ksailConfig, shared.Distribution)

	provisioner, err := factory.Provisioner(distribution, ksailConfig)
	if err != nil {
		return err
	}

	fmt.Printf("🔥 Destroying '%s'...\n", name)
	exists, err := provisioner.Exists(name)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("✔ no cluster named '%s' found\n", name)
		return nil
	}
	if err := provisioner.Delete(name); err != nil {
		return err
	}
	fmt.Printf("✔ cluster named '%s' destroyed\n", name)
	return nil
}

func init() {
	rootCmd.AddCommand(downCmd)
  shared.AddNameFlag(downCmd)
  shared.AddDistributionFlag(downCmd)
}

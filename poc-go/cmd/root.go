package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ksail",
	Short: "KSail - Kubernetes SDK for Local GitOps Development",
	Long: `KSail is a unified SDK for spinning up local Kubernetes clusters and managing workloads declaratively.
It streamlines Kubernetes development by providing a single interface over multiple container engines,
distributions, and deployment tools.

This is a Go-based proof of concept that demonstrates using native Go libraries instead of external binaries.`,
	Version: "0.1.0-poc",
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Add global flags
	rootCmd.PersistentFlags().StringP("context", "c", "", "Kubernetes context to use")
	rootCmd.PersistentFlags().StringP("kubeconfig", "k", "", "Path to kubeconfig file")
	rootCmd.PersistentFlags().IntP("timeout", "t", 300, "Timeout in seconds")
	
	// Add subcommands
	rootCmd.AddCommand(newInitCommand())
	rootCmd.AddCommand(newUpCommand())
	rootCmd.AddCommand(newDownCommand())
	rootCmd.AddCommand(newStatusCommand())
	rootCmd.AddCommand(newListCommand())
	rootCmd.AddCommand(newStartCommand())
	rootCmd.AddCommand(newStopCommand())
	rootCmd.AddCommand(newUpdateCommand())
	rootCmd.AddCommand(newConnectCommand())
	rootCmd.AddCommand(newValidateCommand())
	rootCmd.AddCommand(newGenCommand())
	rootCmd.AddCommand(newSecretsCommand())
}

// Helper function to handle errors consistently
func handleError(err error) {
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
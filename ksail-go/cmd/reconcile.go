/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/spf13/cobra"
)

// reconcileCmd represents the reconcile command
var reconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleReconcile()
	},
}

// --- internals ---

func handleReconcile() error {
  InitServices()
	err := configValidator.Validate()
	if err != nil {
		return err
	}
  // TODO: Validate workloads
  // TODO: Reconcile
	return nil
}

func init() {
	rootCmd.AddCommand(reconcileCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// reconcileCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// reconcileCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

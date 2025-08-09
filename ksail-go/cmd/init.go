package cmd

import (
	"devantler.tech/ksail/cmd/handler"
	"devantler.tech/ksail/cmd/helper"
	ksail "devantler.tech/ksail/internal/utils"
	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	yamlMarshaller "devantler.tech/ksail/pkg/marshaller/yaml"
	"github.com/spf13/cobra"
)

var (
	name         string               = "ksail-default"
	distribution cluster.Distribution = cluster.DistributionKind
	output       string               = "./"
	srcDir       string               = "k8s"
	force        bool                 = false
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a new project",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		var marshaller = yamlMarshaller.NewYamlMarshaller[*cluster.Cluster]()
		var clusterLoader *ksail.ClusterLoader = ksail.NewClusterLoader(marshaller)

		clusterObj := helper.LoadConfiguration(cmd, clusterLoader, output, name, distribution, srcDir)
		handler.Scaffold(cmd, clusterObj, output, force)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVarP(&output, "output", "o", "./", "Output directory")
	initCmd.Flags().StringVarP(&name, "name", "n", "ksail-default", "Name of the KSail cluster")
	initCmd.Flags().VarP(&distribution, "distribution", "d", "Kubernetes distribution to use (kind, k3d, talos-in-docker)")
	initCmd.Flags().StringVarP(&srcDir, "source-directory", "", "k8s", "Relative path to the source directory")
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing files if present")
}

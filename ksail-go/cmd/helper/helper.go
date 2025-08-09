package helper

import (
	"os"

	ksail "devantler.tech/ksail/internal/utils"
	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

func LoadConfiguration(cmd *cobra.Command, clusterLoader *ksail.ClusterLoader, output string, name string, distribution cluster.Distribution, srcDir string) cluster.Cluster {
	cmd.Println("⏳ Loading configuration...")
	clusterObj, err := clusterLoader.LoadCluster(output)
	if err != nil {
		cmd.PrintErrln("\033[31m" + err.Error() + "\033[0m")
		os.Exit(1)
	}

	SetInitialValuesFromInput(clusterObj, name, distribution, srcDir)

	cmd.Println("✔ configuration loaded")
	cmd.Println("")
	return *clusterObj
}

func SetInitialValuesFromInput(clusterObj *cluster.Cluster, name string, distribution cluster.Distribution, srcDir string) {
	clusterObj.Metadata.Name = name
	clusterObj.Spec.Distribution = distribution
	clusterObj.Spec.DistributionConfigPath = GetDistributionConfigPath(clusterObj.Spec.Distribution)
	clusterObj.Spec.SourceDirectory = srcDir
}

func GetDistributionConfigPath(distribution cluster.Distribution) string {
	switch distribution {
	case cluster.DistributionKind:
		return "kind.yaml"
	case cluster.DistributionK3d:
		return "k3d.yaml"
	case cluster.DistributionTalosInDocker:
		return "talos/"
	default:
		return "kind.yaml"
	}
}

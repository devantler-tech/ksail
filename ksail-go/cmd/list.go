package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/cmd/inputs"
	"github.com/devantler-tech/ksail/internal/ui/quiet"
	ksailcluster "github.com/devantler-tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

// listCmd lists clusters from the current distribution or all when --all is set.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List running clusters",
	Long: `List running clusters.

  Defaults to listing all clusters from the distribution selected in the nearest 'ksail.yaml'. To list clusters from all distributions, use --all.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return handleList()
	},
}

// --- internals ---

func handleList() error {
	if err := quiet.SilenceStdout(func() error {
    InitServices()
		return nil
	}); err != nil {
		return err
	}
	return list()
}

// list lists clusters from the given cfg.
func list() error {
	var distributions []ksailcluster.Distribution
	if inputs.All {
		distributions = []ksailcluster.Distribution{ksailcluster.DistributionKind, ksailcluster.DistributionK3d}
	} else {
		distributions = []ksailcluster.Distribution{ksailConfig.Spec.Distribution}
	}
	clusterDistributionPairs, err := fetchClusterDistributionPairs(distributions)
	if err != nil {
		return err
	}
	return displayClusterDistributionPairs(clusterDistributionPairs)
}

// fetchClusterDistributionPairs retrieves the list of clusters for the given distributions.
func fetchClusterDistributionPairs(distributions []ksailcluster.Distribution) ([][2]string, error) {
	clusterDistributionPair := make([][2]string, 0)
	for _, distribution := range distributions {
		ksailConfig.Spec.Distribution = distribution
		containerEngineProvisioner.CheckReady()
		clusters, err := clusterProvisioner.List()
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			clusterDistributionPair = append(clusterDistributionPair, [2]string{c, distribution.String()})
		}
	}
	return clusterDistributionPair, nil
}

// displayClusterDistributionPairs renders the clusters and their distribution in a list of strings.
func displayClusterDistributionPairs(clusterDistributionPairs [][2]string) error {
	if len(clusterDistributionPairs) != 0 {
		for _, r := range clusterDistributionPairs {
			fmt.Printf("%s, %s\n", r[0], r[1])
		}
	} else {
		fmt.Println("✔ no clusters found")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(listCmd)
	inputs.AddDistributionFlag(listCmd)
	inputs.AddAllFlag(listCmd, "include clusters from all distributions")
}

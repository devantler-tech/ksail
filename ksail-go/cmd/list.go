package cmd

import (
	"fmt"

	"devantler.tech/ksail/cmd/shared"
	factory "devantler.tech/ksail/internal"
	"devantler.tech/ksail/internal/loader"
	"devantler.tech/ksail/internal/ui/quiet"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	clusterprovisioner "devantler.tech/ksail/pkg/provisioner/cluster"
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

// --- internal ---

func handleList() error {
	var ksailConfig ksailcluster.Cluster
	if err := quiet.SilenceStdout(func() error {
		ksailConfig, _ = loader.NewKSailConfigLoader().Load()
		return nil
	}); err != nil {
		return err
	}
	return list(&ksailConfig)
}

func list(ksailConfig *ksailcluster.Cluster) error {

	var distributions []ksailcluster.Distribution
	if shared.All {
		distributions = []ksailcluster.Distribution{ksailcluster.DistributionKind, ksailcluster.DistributionK3d}
	} else {
		distributions = []ksailcluster.Distribution{ksailConfig.Spec.Distribution}
	}

	return renderTable(distributions, ksailConfig)
}

func renderTable(distributions []ksailcluster.Distribution, ksailConfig *ksailcluster.Cluster) error {
	rows := make([][2]string, 0)
	for _, distribution := range distributions {
		var provisioner clusterprovisioner.ClusterProvisioner
		if err := quiet.SilenceStdout(func() error {
			var innerErr error
			provisioner, innerErr = factory.Provisioner(distribution, ksailConfig)
			return innerErr
		}); err != nil {
			return err
		}
		if provisioner == nil {
			continue
		}
		clusters, err := provisioner.List()
		if err != nil {
			return err
		}
		for _, c := range clusters {
			rows = append(rows, [2]string{c, distribution.String()})
		}
	}
	if len(rows) != 0 {
		for _, r := range rows {
			fmt.Printf("%s, %s\n", r[0], r[1])
		}
	} else {
		fmt.Println("✔ no clusters found")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(listCmd)
	shared.AddAllFlag(listCmd)
}

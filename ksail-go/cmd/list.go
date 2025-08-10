package cmd

import (
	"fmt"
	"strings"

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
		distributions = []ksailcluster.Distribution{ksailcluster.DistributionKind, ksailcluster.DistributionK3d, ksailcluster.DistributionTind}
	} else {
		distributions = []ksailcluster.Distribution{ksailConfig.Spec.Distribution}
	}

	return renderTable(distributions, ksailConfig)
}

func renderTable(distributions []ksailcluster.Distribution, ksailConfig *ksailcluster.Cluster) error {
	rows := make([][2]string, 0)
	for _, distribution := range distributions {
		var provisioner clusterprovisioner.ClusterProvisioner
		var err error
		if err := quiet.SilenceStdout(func() error {
			provisioner, err = factory.Provisioner(distribution, ksailConfig)
			return nil
		}); err != nil {
			return err
		}
		clusters, err := provisioner.List()
		if err != nil {
			return err
		}
		for _, c := range clusters {
			rows = append(rows, [2]string{distribution.String(), c})
		}
	}

	headers := [2]string{"DISTRIBUTION", "NAME"}
	widths := [2]int{len(headers[0]), len(headers[1])}
	for _, r := range rows {
		if len(r[0]) > widths[0] {
			widths[0] = len(r[0])
		}
		if len(r[1]) > widths[1] {
			widths[1] = len(r[1])
		}
	}

	// Build a format string with the computed widths
	fmtStr := fmt.Sprintf("%%-%ds  %%-%ds\n", widths[0], widths[1])
	// Print header
	fmt.Printf(fmtStr, headers[0], headers[1])
	// Print separator
	fmt.Println(strings.Repeat("-", widths[0]) + "  " + strings.Repeat("-", widths[1]))
	// Print rows
	for _, r := range rows {
		fmt.Printf(fmtStr, r[0], r[1])
	}
	return nil
}

func init() {
	rootCmd.AddCommand(listCmd)
	shared.AddAllFlag(listCmd)
}

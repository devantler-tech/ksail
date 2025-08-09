package cmd

import (
	"fmt"
	"os"
	"strings"

	"devantler.tech/ksail/internal/ui/notify"
	"devantler.tech/ksail/internal/ui/quiet"
	"devantler.tech/ksail/internal/util"
	ksailcluster "devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	kindProvisioner "devantler.tech/ksail/pkg/provisioner/cluster/kind"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var listAll bool

// listCmd lists clusters from the current distribution or all when --all is set.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List running clusters",
	Long: `List running clusters.

  Defaults to listing all clusters from the distribution selected in the nearest 'ksail.yaml'. To list clusters from all distributions, use --all.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := ListClusters(listAll); err != nil {
			notify.Errorf("%s", err)
			os.Exit(1)
		}
	},
}

func ListClusters(all bool) error {
	// Silence stdout while loading configs to avoid noisy loader prints for this command
	var ksailConfig *ksailcluster.Cluster
	if err := quiet.SilenceStdout(func() error {
		var err error
		ksailConfig, err = util.NewKSailConfigLoader().LoadKSailConfig()
		return err
	}); err != nil {
		return err
	}
	var kindConfig *v1alpha4.Cluster
	_ = quiet.SilenceStdout(func() error {
		// ignore any error; kind config is not required for listing
		kindConfig, _ = util.NewKindConfigLoader().LoadKindConfig()
		return nil
	})

	// Decide which distributions to list
	var dists []ksailcluster.Distribution
	if all {
		dists = []ksailcluster.Distribution{ksailcluster.DistributionKind, ksailcluster.DistributionK3d, ksailcluster.DistributionTind}
	} else {
		dists = []ksailcluster.Distribution{ksailConfig.Spec.Distribution}
	}

	// Collect rows for table rendering: [distribution, name]
	rows := make([][2]string, 0)
	for _, d := range dists {
		switch d {
		case ksailcluster.DistributionKind:
			prov := kindProvisioner.NewKindClusterProvisioner(ksailConfig, kindConfig)
			clusters, err := prov.List()
			if err != nil {
				return err
			}
			for _, c := range clusters {
				rows = append(rows, [2]string{"kind", c})
			}
		case ksailcluster.DistributionK3d:
			// TODO: implement when k3d provisioner supports List
		case ksailcluster.DistributionTind:
			// TODO: implement when tind provisioner supports List
		}
	}

	if len(rows) == 0 {
		fmt.Println("► No clusters found.")
		return nil
	}

	// Render a simple table
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

// quiet.SilenceStdout moved to internal/ui/quiet

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "List clusters from all distributions")
}

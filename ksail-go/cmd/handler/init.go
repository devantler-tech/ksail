package handler

import (
	ksail "devantler.tech/ksail/internal/utils"
	"devantler.tech/ksail/pkg/apis/v1alpha1/cluster"
	"github.com/spf13/cobra"
)

func Scaffold(cmd *cobra.Command, clusterObj cluster.Cluster, output string, force bool) {
	scaffolder := ksail.NewScaffolder(clusterObj)
	cmd.Println("📝 Scaffolding new project...")
	scaffolder.Scaffold(output, force)
	cmd.Println("✔ project scaffolded")
}

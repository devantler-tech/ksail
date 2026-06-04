package cluster

import (
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/spf13/cobra"
)

// NewInfoCmd creates the cluster info command.
// The command queries the infrastructure provider API first, then attempts
// kubectl cluster-info, and only fails if no information is available at all.
func NewInfoCmd(_ *di.Runtime) *cobra.Command {
	var (
		nameFlag     string
		providerFlag v1alpha1.Provider
	)

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Display cluster information",
		Long: "Display cluster information from the infrastructure provider" +
			" and Kubernetes API. Succeeds if information is available from any source.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInfoCmd(cmd, nameFlag, providerFlag)
		},
	}

	cmd.Flags().StringVarP(
		&nameFlag,
		"name",
		"n",
		"",
		"Name of the cluster to target",
	)

	cmd.Flags().VarP(
		&providerFlag,
		"provider",
		"p",
		fmt.Sprintf("Provider to use (%s)", providerFlag.ValidValues()),
	)

	return cmd
}

// diagnoseLongDesc describes the `ksail cluster diagnose` command.
const diagnoseLongDesc = `Surface failing Kubernetes resources for the ` +
	`current cluster.

This command inspects the live cluster via the Kubernetes API and reports
any pods that are not running successfully, any nodes that are not Ready,
and any PersistentVolumeClaims stuck in Pending phase. Each finding
includes a severity classification and, where a known failure pattern is
detected, a proactive remediation suggestion.

A health score (0–100) summarises overall cluster health: each critical
finding deducts 25 points and each warning deducts 10 points.

The output is intentionally compact so it can be consumed directly by users
or by the KSail AI chat assistant (ksail chat) and MCP server, which expose
this command as part of the cluster_read tool. When used from the AI
assistant the output is fed back as context so Copilot can explain the root
cause and suggest remediation.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

Exit code 0 is returned even when pod or node failures are reported.
A non-zero exit code indicates the Kubernetes API could not be queried
(e.g., the cluster is unreachable or the credentials lack sufficient
permissions).`

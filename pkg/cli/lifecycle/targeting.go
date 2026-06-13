package lifecycle

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/cobra"
)

// ClusterTargetingHelp is the canonical resolution-priority block describing how
// cluster-targeting commands pick a cluster, provider, and kubeconfig. Commands
// compose it into their Long description instead of copy-pasting (and drifting)
// the priority text. It is intentionally written as a standalone paragraph so it
// reads correctly when appended to a command's own description.
const ClusterTargetingHelp = `The cluster is resolved in the following priority order:
  1. From the --name flag
  2. From metadata.name in the ksail.yaml config file (if present)
  3. From the current kubeconfig context

The provider is resolved in the following priority order:
  1. From the --provider flag
  2. From the ksail.yaml config file (if present)
  3. Defaults to Docker

The kubeconfig is resolved in the following priority order:
  1. From the --kubeconfig flag
  2. From the KUBECONFIG environment variable
  3. From the ksail.yaml config file (if present)
  4. Defaults to ~/.kube/config`

// WithClusterTargetingHelp appends the shared cluster-targeting resolution block
// to a command's description, separated by a blank line. Pass the command's own
// lead paragraph; the priority block is single-sourced via ClusterTargetingHelp.
func WithClusterTargetingHelp(lead string) string {
	lead = strings.TrimRight(lead, "\n")
	if lead == "" {
		return ClusterTargetingHelp
	}

	return lead + "\n\n" + ClusterTargetingHelp
}

// ClusterTargetFlags holds the bound values for the shared cluster-targeting
// flags (--name, --provider, --kubeconfig). Commands that target a cluster by
// name embed it and pass its fields to ResolveClusterInfo.
type ClusterTargetFlags struct {
	Name       string
	Provider   v1alpha1.Provider
	Kubeconfig string
}

// RegisterClusterTargetFlags registers the shared --name (-n), --provider (-p),
// and --kubeconfig (-k) flags used by cluster-targeting commands and binds them
// to flags. This single-sources the registration that delete previously inlined
// and that connect/diff partially duplicated, keeping the shorthand assignments
// (-n/-p/-k) consistent across the cluster group.
//
// nameUsage and kubeconfigUsage let callers tailor the help text to the command
// (e.g. "to delete" vs "to target"); pass "" to use the shared defaults.
func RegisterClusterTargetFlags(
	cmd *cobra.Command,
	flags *ClusterTargetFlags,
	nameUsage, kubeconfigUsage string,
) {
	if nameUsage == "" {
		nameUsage = "Name of the cluster to target"
	}

	if kubeconfigUsage == "" {
		kubeconfigUsage = "Path to kubeconfig file"
	}

	cmd.Flags().StringVarP(&flags.Name, "name", "n", "", nameUsage)
	cmd.Flags().VarP(&flags.Provider, "provider", "p",
		fmt.Sprintf("Provider to use (%s)", strings.Join(flags.Provider.ValidValues(), ", ")))
	cmd.Flags().StringVarP(&flags.Kubeconfig, "kubeconfig", "k", "", kubeconfigUsage)
}

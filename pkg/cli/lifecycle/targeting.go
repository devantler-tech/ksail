package lifecycle

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/cobra"
)

// The resolution-priority blocks are single-sourced as segments so commands can
// compose exactly the blocks for the flags they actually expose (e.g. connect
// has no --provider flag), without duplicating the priority text.
const (
	clusterResolutionHelp = `The cluster is resolved in the following priority order:
  1. From the --name flag
  2. From metadata.name in the ksail.yaml config file (if present)
  3. From the current kubeconfig context`

	providerResolutionHelp = `The provider is resolved in the following priority order:
  1. From the --provider flag
  2. From the ksail.yaml config file (if present)
  3. Defaults to Docker`

	kubeconfigResolutionHelp = `The kubeconfig is resolved in the following priority order:
  1. From the --kubeconfig flag
  2. From the KUBECONFIG environment variable
  3. From the ksail.yaml config file (if present)
  4. Defaults to ~/.kube/config`
)

// ClusterTargetingHelp is the canonical resolution-priority block for commands
// that expose all three targeting flags (--name, --provider, --kubeconfig).
const ClusterTargetingHelp = clusterResolutionHelp + "\n\n" + providerResolutionHelp +
	"\n\n" + kubeconfigResolutionHelp

// ClusterTargetingHelpWithoutProvider is ClusterTargetingHelp for commands that
// target a cluster by name + kubeconfig but do NOT expose a --provider flag (e.g.
// connect, which derives the provider from config). It omits the provider block
// so --help does not advertise a flag the command lacks.
const ClusterTargetingHelpWithoutProvider = clusterResolutionHelp + "\n\n" + kubeconfigResolutionHelp

// WithClusterTargetingHelp appends the shared cluster-targeting resolution block
// to a command's description, separated by a blank line. Pass the command's own
// lead paragraph; the priority block is single-sourced via ClusterTargetingHelp.
func WithClusterTargetingHelp(lead string) string {
	return appendHelpBlock(lead, ClusterTargetingHelp)
}

// WithClusterTargetingHelpWithoutProvider is WithClusterTargetingHelp for commands
// that do not expose a --provider flag (the provider block is omitted).
func WithClusterTargetingHelpWithoutProvider(lead string) string {
	return appendHelpBlock(lead, ClusterTargetingHelpWithoutProvider)
}

// appendHelpBlock joins a command's lead paragraph and a resolution block with a
// blank line, returning the block alone when the lead is empty.
func appendHelpBlock(lead, block string) string {
	lead = strings.TrimRight(lead, "\n")
	if lead == "" {
		return block
	}

	return lead + "\n\n" + block
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

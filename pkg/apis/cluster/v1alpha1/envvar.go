package v1alpha1

import "github.com/devantler-tech/ksail/v7/pkg/envvar"

// ExpandEnvVars expands environment variable placeholders in all string fields
// of the cluster configuration. This includes paths, credentials, contexts, and
// other configuration values.
//
// Placeholders use the format ${VAR_NAME}. If a referenced environment variable
// is not set, the placeholder is replaced with an empty string.
//
// This method should be called after unmarshaling the configuration to ensure
// all user-facing string values support environment variable expansion.
func (c *Cluster) ExpandEnvVars() {
	c.expandSpec()
}

func (c *Cluster) expandSpec() {
	// Expand top-level Spec fields
	c.Spec.Editor = envvar.Expand(c.Spec.Editor)

	// Expand ClusterSpec fields
	c.expandClusterSpec()

	// Expand ProviderSpec fields
	c.expandProviderSpec()

	// Expand WorkloadSpec fields
	c.expandWorkloadSpec()

	// Expand ChatSpec fields
	c.expandChatSpec()
}

func (c *Cluster) expandClusterSpec() {
	cluster := &c.Spec.Cluster

	// Expand cluster-level fields
	cluster.DistributionConfig = envvar.Expand(cluster.DistributionConfig)

	// Expand Connection fields
	cluster.Connection.Kubeconfig = envvar.Expand(cluster.Connection.Kubeconfig)
	cluster.Connection.Context = envvar.Expand(cluster.Connection.Context)

	// Expand entire registry string including credentials and host. When
	// ResolveCredentials() is called later, the envvar.Expand() calls within it
	// become no-ops since expansion has already occurred.
	cluster.LocalRegistry.Registry = envvar.Expand(cluster.LocalRegistry.Registry)

	// Note: SOPS.AgeKeyEnvVar is the name of the env var itself, not a value to expand

	// Expand distribution-specific options
	c.expandVanillaOptions()
	c.expandTalosOptions()
}

func (c *Cluster) expandVanillaOptions() {
	vanilla := &c.Spec.Cluster.Vanilla
	vanilla.MirrorsDir = envvar.Expand(vanilla.MirrorsDir)
}

func (c *Cluster) expandTalosOptions() {
	talos := &c.Spec.Cluster.Talos
	talos.Config = envvar.Expand(talos.Config)
}

func (c *Cluster) expandProviderSpec() {
	c.expandHetznerOptions()
}

func (c *Cluster) expandHetznerOptions() {
	hetzner := &c.Spec.Provider.Hetzner
	hetzner.SSHKeyName = envvar.Expand(hetzner.SSHKeyName)
	hetzner.NetworkName = envvar.Expand(hetzner.NetworkName)
	hetzner.PlacementGroup = envvar.Expand(hetzner.PlacementGroup)
	// Note: TokenEnvVar is the name of the env var itself, not a value to expand
	// So we don't expand it here
}

func (c *Cluster) expandWorkloadSpec() {
	c.Spec.Workload.SourceDirectory = envvar.Expand(c.Spec.Workload.SourceDirectory)
	c.Spec.Workload.Tag = envvar.Expand(c.Spec.Workload.Tag)
	c.Spec.Workload.KustomizationFile = envvar.Expand(c.Spec.Workload.KustomizationFile)
}

func (c *Cluster) expandChatSpec() {
	c.Spec.Chat.Model = envvar.Expand(c.Spec.Chat.Model)
}

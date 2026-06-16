package lifecycle

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
)

// clusterNameOptions configures ResolveClusterName. The zero value is the
// standard CLI ordering: kubeconfig context > metadata.name > distribution config.
type clusterNameOptions struct {
	// distConfigFirst flips the ordering so the distribution config is the
	// primary source. Used by the Omni kubeconfig hook, where the distconfig
	// name takes precedence over any kubeconfig-derived name and metadata.name /
	// connection.context are not consulted.
	distConfigFirst bool

	// skipContext disables deriving the name from clusterCfg connection.context.
	skipContext bool

	// skipMetadataName disables the metadata.name source.
	skipMetadataName bool

	// fallback supplies a final name source consulted when every enabled source
	// above yields nothing (e.g. the Omni hook's kubeconfig current-context).
	fallback func() string
}

// ClusterNameOption customizes how ResolveClusterName resolves the cluster name.
type ClusterNameOption func(*clusterNameOptions)

// WithDistConfigPriority makes the distribution config the primary name source
// and disables the connection.context and metadata.name sources. This encodes
// the Omni kubeconfig hook's distconfig-over-kubeconfig priority without forking
// the resolver: the distribution config name wins, and a configured fallback
// (kubeconfig current-context) is consulted only when it is empty.
func WithDistConfigPriority() ClusterNameOption {
	return func(opts *clusterNameOptions) {
		opts.distConfigFirst = true
		opts.skipContext = true
		opts.skipMetadataName = true
	}
}

// WithClusterNameFallback supplies a final name source consulted when the
// preceding sources yield nothing. Used by the Omni hook to fall back to the
// kubeconfig current-context name.
func WithClusterNameFallback(fallback func() string) ClusterNameOption {
	return func(opts *clusterNameOptions) {
		opts.fallback = fallback
	}
}

// ResolveClusterName is the single owner of cluster-name resolution.
//
// By default it resolves in priority order:
//  1. The cluster name derived from clusterCfg's connection.context (e.g.
//     "kind-my-cluster" → "my-cluster"), when a context is set.
//  2. clusterCfg.metadata.name, when present and valid.
//  3. The distribution config name (via configmanager.GetClusterName).
//
// distConfig may be nil (sources that need it are skipped) or any type accepted
// by configmanager.GetClusterName. Options reorder or disable sources for
// callers with different priorities (e.g. WithDistConfigPriority for Omni).
func ResolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	distConfig any,
	opts ...ClusterNameOption,
) (string, error) {
	options := clusterNameOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	if options.distConfigFirst {
		if name := distConfigName(distConfig); name != "" {
			return name, nil
		}
	}

	if name := contextOrMetadataName(clusterCfg, options); name != "" {
		return name, nil
	}

	if !options.distConfigFirst {
		name, err := distConfigNameOrError(distConfig, options.fallback != nil)
		if err != nil {
			return "", err
		}

		if name != "" {
			return name, nil
		}
	}

	if options.fallback != nil {
		return options.fallback(), nil
	}

	return "", nil
}

// contextOrMetadataName resolves the connection.context and metadata.name
// sources subject to the enabled options. Returns an empty string when neither
// source is enabled or yields a value.
func contextOrMetadataName(clusterCfg *v1alpha1.Cluster, options clusterNameOptions) string {
	if !options.skipContext {
		if name := extractClusterNameFromContext(clusterCfg); name != "" {
			return name
		}
	}

	if options.skipMetadataName || clusterCfg == nil || clusterCfg.Name == "" {
		return ""
	}

	if v1alpha1.ValidateClusterName(clusterCfg.Name) == nil {
		return clusterCfg.Name
	}

	return ""
}

// distConfigNameOrError extracts the distribution-config name. When hasFallback
// is false, a configmanager error is surfaced (preserving the standard
// handlers' behavior); when a fallback exists, the error is swallowed so the
// fallback is tried instead.
func distConfigNameOrError(distConfig any, hasFallback bool) (string, error) {
	name, err := configmanager.GetClusterName(distConfig)
	if err == nil {
		return name, nil
	}

	if hasFallback {
		return "", nil
	}

	return "", fmt.Errorf("failed to get cluster name from distribution config: %w", err)
}

// distConfigName extracts the cluster name from a distribution config without
// surfacing the unsupported-type error (used by the distConfigFirst path, which
// treats an unresolved name as "fall through to the next source").
func distConfigName(distConfig any) string {
	name, err := configmanager.GetClusterName(distConfig)
	if err != nil {
		return ""
	}

	return name
}

// ClusterNameFromDistributionConfig extracts the cluster name from a
// provisioner DistributionConfig by delegating to configmanager.GetClusterName
// for whichever distribution-specific config is populated. It is the shared
// building block behind the --name/config resolution used by simple lifecycle
// commands and the Omni kubeconfig hook.
//
// Returns an empty string when distConfig is nil or no distribution config is set.
func ClusterNameFromDistributionConfig(distConfig *clusterprovisioner.DistributionConfig) string {
	if distConfig == nil {
		return ""
	}

	for _, cfg := range []any{
		nilable(distConfig.Kind),
		nilable(distConfig.K3d),
		nilable(distConfig.Talos),
		nilable(distConfig.VCluster),
		nilable(distConfig.KWOK),
		nilable(distConfig.EKS),
	} {
		if cfg == nil {
			continue
		}

		name, err := configmanager.GetClusterName(cfg)
		if err == nil && name != "" {
			return name
		}
	}

	return ""
}

// nilable normalizes a typed nil pointer to an untyped nil so the
// configmanager.GetClusterName type switch is only reached for populated
// configs.
func nilable[T any](ptr *T) any {
	if ptr == nil {
		return nil
	}

	return ptr
}

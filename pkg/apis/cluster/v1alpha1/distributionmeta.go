package v1alpha1

import "slices"

// providerCapability describes for which providers a distribution provides a
// component (CSI, LoadBalancer, ...) by default. The zero value means "no
// provider".
//
// +kubebuilder:object:generate=false
type providerCapability struct {
	// all marks the capability as provided regardless of provider.
	all bool
	// providers lists the specific providers the capability is provided for.
	providers []Provider
}

// has reports whether the capability is provided for the given provider.
func (c providerCapability) has(provider Provider) bool {
	return c.all || slices.Contains(c.providers, provider)
}

// allProviders returns a capability provided regardless of provider.
func allProviders() providerCapability {
	return providerCapability{all: true}
}

// onlyProviders returns a capability provided for the listed providers only.
func onlyProviders(providers ...Provider) providerCapability {
	return providerCapability{providers: providers}
}

// distributionMeta is the single source of per-distribution knowledge:
// config file name, default cluster/context naming, supported providers, and
// which components the distribution provides by default. Adding a
// distribution means adding one const and one row to distributionMetas.
//
// +kubebuilder:object:generate=false
type distributionMeta struct {
	// configFile is the default distribution configuration file or directory.
	configFile string
	// defaultClusterName is the default cluster name.
	defaultClusterName string
	// contextPrefix is prepended to the cluster name to form the kubeconfig context.
	contextPrefix string
	// contextSuffix is appended to the cluster name to form the kubeconfig context.
	contextSuffix string
	// supportedProviders lists the providers the distribution can run on.
	supportedProviders []Provider
	// providesCDI reports whether CDI is enabled by default.
	providesCDI bool
	// providesMetricsServer reports whether metrics-server is bundled by default.
	providesMetricsServer bool
	// providesStorage reports whether a default storage provisioner is bundled.
	providesStorage bool
	// csiByDefault describes per-provider default CSI support.
	csiByDefault providerCapability
	// loadBalancerByDefault describes per-provider default LoadBalancer support.
	loadBalancerByDefault providerCapability
}

// distributionMetas returns the per-distribution metadata table. A fresh map
// is built on each call so callers can never mutate shared state.
func distributionMetas() map[Distribution]distributionMeta {
	return map[Distribution]distributionMeta{
		DistributionVanilla: {
			configFile:         DefaultVanillaDistributionConfig,
			defaultClusterName: "kind",
			contextPrefix:      "kind-",
			supportedProviders: []Provider{ProviderDocker, ProviderKubernetes},
		},
		DistributionK3s: {
			configFile:            DefaultK3sDistributionConfig,
			defaultClusterName:    "k3d-default",
			contextPrefix:         "k3d-",
			supportedProviders:    []Provider{ProviderDocker, ProviderKubernetes},
			providesMetricsServer: true,
			providesStorage:       true,
			csiByDefault:          allProviders(),
			loadBalancerByDefault: allProviders(),
		},
		DistributionTalos: {
			configFile:         DefaultTalosDistributionConfig,
			defaultClusterName: "talos-default",
			contextPrefix:      "admin@",
			supportedProviders: []Provider{
				ProviderDocker, ProviderHetzner, ProviderOmni, ProviderKubernetes,
			},
			providesCDI:           true,
			csiByDefault:          onlyProviders(ProviderHetzner),
			loadBalancerByDefault: onlyProviders(ProviderHetzner),
		},
		DistributionVCluster: {
			configFile:            DefaultVClusterDistributionConfig,
			defaultClusterName:    "vcluster-default",
			contextPrefix:         "vcluster-docker_",
			supportedProviders:    []Provider{ProviderDocker, ProviderKubernetes},
			loadBalancerByDefault: allProviders(),
		},
		DistributionKWOK: {
			configFile:         DefaultKWOKDistributionConfig,
			defaultClusterName: "kwok-default",
			contextPrefix:      "kwok-",
			supportedProviders: []Provider{ProviderDocker, ProviderKubernetes},
		},
		DistributionEKS: {
			configFile:            DefaultEKSDistributionConfig,
			defaultClusterName:    "eks-default",
			contextSuffix:         ".eksctl.io",
			supportedProviders:    []Provider{ProviderAWS},
			providesStorage:       true,
			csiByDefault:          onlyProviders(ProviderAWS),
			loadBalancerByDefault: onlyProviders(ProviderAWS),
		},
	}
}

// distributionMetaFor looks up the metadata row for a distribution. The
// second return value is false for unknown distributions; callers fall back
// to their documented defaults in that case.
func distributionMetaFor(distribution Distribution) (distributionMeta, bool) {
	meta, found := distributionMetas()[distribution]

	return meta, found
}

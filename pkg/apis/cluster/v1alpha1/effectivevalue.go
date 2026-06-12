package v1alpha1

// This file holds the EffectiveValue resolution for the tri-state component
// toggles (CSI, CDI, MetricsServer, LoadBalancer): Default (or empty) resolves
// to Enabled/Disabled depending on what the distribution × provider
// combination provides by default; explicit values pass through unchanged.

// effectiveToggle implements the shared EffectiveValue resolution for
// tri-state component toggles: explicit non-default values pass through
// unchanged, while defaultValue (or the empty string) resolves to enabled or
// disabled depending on whether the distribution provides the component by
// default.
func effectiveToggle[T ~string](value, defaultValue, enabled, disabled T, provided bool) T {
	if value != defaultValue && value != "" {
		return value
	}

	if provided {
		return enabled
	}

	return disabled
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that bundle a CSI driver (e.g. K3s),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (c *CSI) EffectiveValue(distribution Distribution, provider Provider) CSI {
	return effectiveToggle(
		*c, CSIDefault, CSIEnabled, CSIDisabled,
		distribution.ProvidesCSIByDefault(provider),
	)
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that enable CDI by default (e.g. Talos 1.13+),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (c *CDI) EffectiveValue(distribution Distribution, _ Provider) CDI {
	return effectiveToggle(
		*c, CDIDefault, CDIEnabled, CDIDisabled,
		distribution.ProvidesCDIByDefault(),
	)
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that bundle metrics-server (e.g. K3s),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (m *MetricsServer) EffectiveValue(distribution Distribution, _ Provider) MetricsServer {
	return effectiveToggle(
		*m, MetricsServerDefault, MetricsServerEnabled, MetricsServerDisabled,
		distribution.ProvidesMetricsServerByDefault(),
	)
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that bundle a load balancer (e.g. K3s),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (l *LoadBalancer) EffectiveValue(
	distribution Distribution, provider Provider,
) LoadBalancer {
	return effectiveToggle(
		*l, LoadBalancerDefault, LoadBalancerEnabled, LoadBalancerDisabled,
		distribution.ProvidesLoadBalancerByDefault(provider),
	)
}

package v1alpha1 //nolint:dupl // enum types follow a consistent pattern by design

import (
	"fmt"
	"strings"
)

// LoadBalancer defines the LoadBalancer options for a KSail cluster.
type LoadBalancer string

const (
	// LoadBalancerDefault relies on the distribution × provider default behavior for LoadBalancer support.
	LoadBalancerDefault LoadBalancer = "Default"
	// LoadBalancerEnabled ensures LoadBalancer support is enabled.
	LoadBalancerEnabled LoadBalancer = "Enabled"
	// LoadBalancerDisabled ensures LoadBalancer support is disabled.
	LoadBalancerDisabled LoadBalancer = "Disabled"
)

// Set for LoadBalancer (pflag.Value interface).
func (l *LoadBalancer) Set(value string) error {
	for _, lb := range ValidLoadBalancers() {
		if strings.EqualFold(value, string(lb)) {
			*l = lb

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidLoadBalancer,
		value,
		LoadBalancerDefault,
		LoadBalancerEnabled,
		LoadBalancerDisabled,
	)
}

// String returns the string representation of the LoadBalancer.
func (l *LoadBalancer) String() string {
	return string(*l)
}

// Type returns the type of the LoadBalancer.
func (l *LoadBalancer) Type() string {
	return "LoadBalancer"
}

// Default returns the default value for LoadBalancer (Default, which defers to the distribution × provider).
func (l *LoadBalancer) Default() any {
	return LoadBalancerDefault
}

// ValidValues returns all valid LoadBalancer values as strings.
func (l *LoadBalancer) ValidValues() []string {
	return []string{
		string(LoadBalancerDefault),
		string(LoadBalancerEnabled),
		string(LoadBalancerDisabled),
	}
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that bundle a load balancer (e.g. K3s),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (l *LoadBalancer) EffectiveValue(
	distribution Distribution, provider Provider,
) LoadBalancer {
	if *l != LoadBalancerDefault && *l != "" {
		return *l
	}

	if distribution.ProvidesLoadBalancerByDefault(provider) {
		return LoadBalancerEnabled
	}

	return LoadBalancerDisabled
}

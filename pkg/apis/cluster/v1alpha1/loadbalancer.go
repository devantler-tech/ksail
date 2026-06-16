package v1alpha1

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

// ValidLoadBalancers returns supported load balancer values.
func ValidLoadBalancers() []LoadBalancer {
	return []LoadBalancer{
		LoadBalancerDefault,
		LoadBalancerEnabled,
		LoadBalancerDisabled,
	}
}

// Set for LoadBalancer (pflag.Value interface).
func (l *LoadBalancer) Set(value string) error {
	return setEnum(l, value, ValidLoadBalancers(), ErrInvalidLoadBalancer)
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
	return validValueStrings(ValidLoadBalancers())
}

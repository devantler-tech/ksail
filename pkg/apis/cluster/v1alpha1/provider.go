package v1alpha1

import (
	"fmt"
	"slices"
	"strings"
)

// Provider defines the infrastructure provider backend for running clusters.
// This is a unified type used across distributions that support multiple providers.
type Provider string

const (
	// ProviderDocker runs cluster nodes as Docker containers.
	ProviderDocker Provider = "Docker"
	// ProviderHetzner runs cluster nodes as Hetzner Cloud servers.
	ProviderHetzner Provider = "Hetzner"
	// ProviderOmni runs cluster nodes managed by Sidero Omni.
	ProviderOmni Provider = "Omni"
)

// Set for Provider (pflag.Value interface).
func (p *Provider) Set(value string) error {
	for _, prov := range ValidProviders() {
		if strings.EqualFold(value, string(prov)) {
			*p = prov

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidProvider,
		value,
		ProviderDocker,
		ProviderHetzner,
		ProviderOmni,
	)
}

// String returns the string representation of the Provider.
func (p *Provider) String() string {
	return string(*p)
}

// Type returns the type of the Provider.
func (p *Provider) Type() string {
	return "Provider"
}

// Default returns the default value for Provider (Docker).
func (p *Provider) Default() any {
	return ProviderDocker
}

// ValidValues returns all valid Provider values as strings.
func (p *Provider) ValidValues() []string {
	return []string{string(ProviderDocker), string(ProviderHetzner), string(ProviderOmni)}
}

// supportedProviders returns the valid providers for a given distribution.
func supportedProviders(distribution Distribution) []Provider {
	switch distribution {
	case DistributionVanilla, DistributionK3s, DistributionVCluster:
		return []Provider{ProviderDocker}
	case DistributionTalos:
		return []Provider{ProviderDocker, ProviderHetzner, ProviderOmni}
	default:
		return nil
	}
}

// ValidateForDistribution validates that the provider is valid for the given distribution.
// Returns nil if the combination is valid, or an error describing the invalid combination.
func (p *Provider) ValidateForDistribution(distribution Distribution) error {
	supported := supportedProviders(distribution)
	if supported == nil {
		return fmt.Errorf("%w: %s", ErrInvalidDistribution, distribution)
	}

	// Empty provider defaults to Docker which is always supported
	if *p == "" || slices.Contains(supported, *p) {
		return nil
	}

	return fmt.Errorf(
		"%w: distribution %s does not support provider %s",
		ErrInvalidDistributionProviderCombination,
		distribution,
		*p,
	)
}

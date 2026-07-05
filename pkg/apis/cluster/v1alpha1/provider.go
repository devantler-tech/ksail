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
	// ProviderAWS runs EKS managed Kubernetes clusters on AWS.
	ProviderAWS Provider = "AWS"
	// ProviderGCP runs GKE managed Kubernetes clusters on Google Cloud.
	ProviderGCP Provider = "GCP"
	// ProviderAzure runs AKS managed Kubernetes clusters on Microsoft Azure.
	ProviderAzure Provider = "Azure"
	// ProviderKubernetes runs cluster nodes as pods inside an existing Kubernetes cluster.
	// Supports all Docker-based distributions (Vanilla, K3s, Talos, VCluster) via either
	// direct pod execution (K3s) or Docker-in-Docker (Kind, Talos, VCluster).
	// Requires Gateway API experimental CRDs and a Gateway controller on the host cluster.
	ProviderKubernetes Provider = "Kubernetes"
)

// ValidProviders returns supported provider values.
func ValidProviders() []Provider {
	return []Provider{
		ProviderDocker,
		ProviderHetzner,
		ProviderOmni,
		ProviderAWS,
		ProviderGCP,
		ProviderAzure,
		ProviderKubernetes,
	}
}

// Set for Provider (pflag.Value interface).
func (p *Provider) Set(value string) error {
	return setEnum(p, value, ValidProviders(), ErrInvalidProvider)
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
	return validValueStrings(ValidProviders())
}

// supportedProviders returns the valid providers for a given distribution.
// The Kubernetes provider is supported by Vanilla, K3s, Talos, VCluster, and KWOK distributions,
// allowing them to run as nested clusters in pod form within an existing host Kubernetes cluster.
// Returns nil for unknown distributions.
func supportedProviders(distribution Distribution) []Provider {
	meta, _ := distributionMetaFor(distribution)

	return slices.Clone(meta.supportedProviders)
}

// DefaultProviderForDistribution returns the conventional default provider for a distribution: the
// first provider it supports (Docker for the local distributions, AWS for EKS). It returns "" for an
// unknown distribution. Callers use this to default an omitted provider before validating, so an
// EKS request without an explicit provider resolves to AWS rather than the global Docker default.
func DefaultProviderForDistribution(distribution Distribution) Provider {
	supported := supportedProviders(distribution)
	if len(supported) == 0 {
		return ""
	}

	return supported[0]
}

// IsCloud returns true if the provider is a cloud provider (Hetzner, Omni, AWS, GCP, or Azure).
// Cloud providers run nodes on remote servers and cannot access local Docker infrastructure.
func (p *Provider) IsCloud() bool {
	return *p == ProviderHetzner || *p == ProviderOmni ||
		*p == ProviderAWS || *p == ProviderGCP || *p == ProviderAzure
}

// NeedsLocalDocker returns true if the provider requires a local Docker daemon
// on the host machine for running cluster nodes, registries, and networks.
// Returns false for cloud providers and the Kubernetes provider (which runs
// nodes as pods in an existing cluster rather than as local Docker containers).
func (p *Provider) NeedsLocalDocker() bool {
	return !p.IsCloud() && *p != ProviderKubernetes
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

	supportedNames := make([]string, len(supported))
	for i, prov := range supported {
		supportedNames[i] = string(prov)
	}

	return fmt.Errorf(
		"%w: distribution %s does not support provider %s (supported providers: %s)",
		ErrInvalidDistributionProviderCombination,
		distribution,
		*p,
		strings.Join(supportedNames, ", "),
	)
}

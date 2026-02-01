package v1alpha1

import "errors"

// ErrInvalidDistribution is returned when an invalid distribution is specified.
var ErrInvalidDistribution = errors.New("invalid distribution")

// ErrInvalidGitOpsEngine is returned when an invalid GitOps engine is specified.
var ErrInvalidGitOpsEngine = errors.New("invalid GitOps engine")

// ErrInvalidCNI is returned when an invalid CNI is specified.
var ErrInvalidCNI = errors.New("invalid CNI")

// ErrInvalidCSI is returned when an invalid CSI is specified.
var ErrInvalidCSI = errors.New("invalid CSI")

// ErrInvalidMetricsServer is returned when an invalid metrics server is specified.
var ErrInvalidMetricsServer = errors.New("invalid metrics server")

// ErrInvalidLoadBalancer is returned when an invalid load balancer option is specified.
var ErrInvalidLoadBalancer = errors.New("invalid load balancer")

// ErrInvalidCertManager is returned when an invalid cert-manager option is specified.
var ErrInvalidCertManager = errors.New("invalid cert-manager")

// ErrInvalidPolicyEngine is returned when an invalid policy engine is specified.
var ErrInvalidPolicyEngine = errors.New("invalid policy engine")

// ErrInvalidProvider is returned when an invalid provider is specified.
var ErrInvalidProvider = errors.New("invalid provider")

// ErrInvalidDistributionProviderCombination is returned when the distribution and provider combination is invalid.
var ErrInvalidDistributionProviderCombination = errors.New(
	"invalid distribution and provider combination",
)

// ErrClusterNameTooLong is returned when the cluster name exceeds the maximum length.
var ErrClusterNameTooLong = errors.New("cluster name is too long")

// ErrClusterNameInvalid is returned when the cluster name is not DNS-1123 compliant.
var ErrClusterNameInvalid = errors.New("cluster name is invalid")

// ErrMirrorRegistryNotSupported is returned when local mirror registries are used with a cloud provider.
var ErrMirrorRegistryNotSupported = errors.New(
	"local mirror registry not supported for cloud provider",
)

// ErrLocalRegistryNotSupported is returned when local registry is used with a cloud provider without external host.
var ErrLocalRegistryNotSupported = errors.New(
	"cloud provider requires an external registry\n" +
		"- use --local-registry with an internet-accessible registry (e.g., ghcr.io/myorg)",
)

// ErrLoadBalancerNotImplemented is returned when LoadBalancer installation is not yet implemented.
var ErrLoadBalancerNotImplemented = errors.New("LoadBalancer installation not yet implemented")

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

// ErrMirrorRegistryNotSupported is returned when mirror registries are used with an unsupported provider.
var ErrMirrorRegistryNotSupported = errors.New(
	"mirror registry configuration not supported for provider",
)

// ErrLocalRegistryNotSupported is returned when local registry is used with a cloud provider without external host.
var ErrLocalRegistryNotSupported = errors.New(
	"local registry configuration not supported for provider",
)

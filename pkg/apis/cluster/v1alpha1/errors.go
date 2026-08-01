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

// ErrInvalidCDI is returned when an invalid CDI is specified.
var ErrInvalidCDI = errors.New("invalid CDI")

// ErrInvalidMetricsServer is returned when an invalid metrics server is specified.
var ErrInvalidMetricsServer = errors.New("invalid metrics server")

// ErrInvalidLoadBalancer is returned when an invalid load balancer option is specified.
var ErrInvalidLoadBalancer = errors.New("invalid load balancer")

// ErrInvalidCertManager is returned when an invalid cert-manager option is specified.
var ErrInvalidCertManager = errors.New("invalid cert-manager")

// ErrInvalidPolicyEngine is returned when an invalid policy engine is specified.
var ErrInvalidPolicyEngine = errors.New("invalid policy engine")

// ErrInvalidImageVerification is returned when an invalid image verification option is specified.
var ErrInvalidImageVerification = errors.New("invalid image verification")

// ErrInvalidNodeAutoscaling is returned when an invalid node autoscaling option is specified.
var ErrInvalidNodeAutoscaling = errors.New("invalid node autoscaling")

// ErrInvalidSOPSEnabled is returned when an invalid spec.cluster.sops.enabled value is specified.
var ErrInvalidSOPSEnabled = errors.New("invalid sops enabled")

// ErrInvalidProvider is returned when an invalid provider is specified.
var ErrInvalidProvider = errors.New("invalid provider")

// ErrInvalidIngressFirewall is returned when an invalid ingress firewall option is specified.
var ErrInvalidIngressFirewall = errors.New("invalid ingress firewall")

// ErrInvalidPlacementGroupStrategy is returned when an invalid placement group strategy is specified.
var ErrInvalidPlacementGroupStrategy = errors.New("invalid placement group strategy")

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

// ErrAWSCredentialsMissing is returned when AWS credentials cannot be resolved via the SDK credential chain.
var ErrAWSCredentialsMissing = errors.New(
	"AWS credentials not found; configure them via 'aws configure', AWS_PROFILE, " +
		"or static AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY",
)

// ErrEksctlBinaryMissing is returned when the eksctl CLI binary is not on PATH.
// Cluster creation on the EKS distribution is delegated to eksctl; see
// https://eksctl.io/installation/ for installation instructions.
var ErrEksctlBinaryMissing = errors.New(
	"eksctl binary not found on PATH; install from https://eksctl.io/installation/",
)

// ErrInvalidPodAutoscalerHorizontal is returned when an invalid pod horizontal autoscaler option is specified.
var ErrInvalidPodAutoscalerHorizontal = errors.New("invalid pod horizontal autoscaler")

// ErrInvalidPodAutoscalerVertical is returned when an invalid pod vertical autoscaler option is specified.
var ErrInvalidPodAutoscalerVertical = errors.New("invalid pod vertical autoscaler")

// ErrInvalidAutoscalerExpander is returned when an invalid autoscaler expander strategy is specified.
var ErrInvalidAutoscalerExpander = errors.New("invalid autoscaler expander")

// ErrDuplicateAutoscalerExpander is returned when the autoscaler expander
// priority list contains the same strategy more than once.
var ErrDuplicateAutoscalerExpander = errors.New("duplicate autoscaler expander")

// ErrInvalidPoolName is returned when a node pool name is not a valid DNS-1123 label.
var ErrInvalidPoolName = errors.New("invalid pool name")

// ErrPoolMinExceedsMax is returned when a node pool min count exceeds its max count.
var ErrPoolMinExceedsMax = errors.New("pool min exceeds max")

// ErrDuplicatePoolName is returned when two or more node pools share the same name.
var ErrDuplicatePoolName = errors.New("duplicate pool name")

// ErrPoolNegativeMin is returned when a node pool min count is negative.
var ErrPoolNegativeMin = errors.New("pool min must be non-negative")

// ErrPoolNegativeMax is returned when a node pool max count is negative.
var ErrPoolNegativeMax = errors.New("pool max must be non-negative")

// ErrPoolServerTypeEmpty is returned when a node pool serverType is empty.
var ErrPoolServerTypeEmpty = errors.New("pool serverType must not be empty")

// ErrPoolLocationEmpty is returned when a node pool location is empty.
var ErrPoolLocationEmpty = errors.New("pool location must not be empty")

// ErrInvalidPoolCapacity is returned when a node pool has a negative min or max value.
var ErrInvalidPoolCapacity = errors.New("invalid pool capacity")

// ErrInvalidPoolLabel is returned when a node pool label key or value is not a
// valid Kubernetes label key/value.
var ErrInvalidPoolLabel = errors.New("invalid pool label")

// ErrInvalidPoolTaint is returned when a node pool taint key, value, or effect is invalid.
var ErrInvalidPoolTaint = errors.New("invalid pool taint")

// ErrInvalidMaxNodesTotal is returned when MaxNodesTotal is negative.
var ErrInvalidMaxNodesTotal = errors.New("invalid maxNodesTotal")

// ErrInvalidScaleDownUtilizationThreshold is returned when
// ScaleDownUtilizationThreshold is not a decimal ratio in the range [0.0, 1.0].
var ErrInvalidScaleDownUtilizationThreshold = errors.New(
	"invalid scaleDownUtilizationThreshold: must be a decimal ratio between 0.0 and 1.0",
)

// ErrInvalidServerLimit is returned when ServerLimit is negative.
var ErrInvalidServerLimit = errors.New("invalid serverLimit")

// ErrAutoscalerExceedsServerLimit is returned when the total node capacity exceeds the
// Hetzner server limit.
var ErrAutoscalerExceedsServerLimit = errors.New(
	"autoscaler configuration exceeds Hetzner server limit",
)

// ErrAutoscalerLeavesNoSnapshotSlot is returned when a Talos + Hetzner cluster can grow to
// occupy every server the account allows, leaving no slot for the temporary server KSail
// boots to build the Talos snapshot the autoscaler needs.
var ErrAutoscalerLeavesNoSnapshotSlot = errors.New(
	"autoscaler configuration leaves no Hetzner server slot for the Talos snapshot build",
)

// ErrExpanderNotSupportedForProvider is returned when the selected autoscaler
// expander strategy is not supported by the infrastructure provider's cloud
// provider implementation (e.g., Hetzner does not implement the pricing API
// required by the Price expander).
var ErrExpanderNotSupportedForProvider = errors.New(
	"autoscaler expander not supported for provider",
)

// ErrAutoscalerEnabledNoPools is returned when node autoscaler is enabled but no pools are configured.
var ErrAutoscalerEnabledNoPools = errors.New(
	"node autoscaler is enabled but no pools are configured",
)

// ErrInvalidAllowedCIDR is returned when an allowed CIDR entry is not a valid CIDR block.
var ErrInvalidAllowedCIDR = errors.New("invalid allowed CIDR")

// ErrInvalidOIDCConfig is returned when the OIDC configuration is incomplete or invalid.
var ErrInvalidOIDCConfig = errors.New("invalid OIDC configuration")

// ErrGatewayAPINotAvailable is returned when Gateway API exposure is requested
// but the host cluster does not have the experimental CRDs installed.
var ErrGatewayAPINotAvailable = errors.New(
	"gateway API experimental CRDs not found on host cluster; " +
		"install from https://gateway-api.sigs.k8s.io/guides/getting-started/#install-experimental-channel",
)

// ErrCIDROverlap is returned when nested cluster CIDRs overlap with host cluster CIDRs.
var ErrCIDROverlap = errors.New("nested cluster CIDR overlaps with host cluster CIDR")

package v1alpha1

import (
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// ClusterNamePattern is the DNS-1123-subdomain regex for cluster names:
// lowercase alphanumeric with optional hyphens, starting with a letter and
// ending with alphanumeric. It is the single source shared by the runtime
// validator (clusterNameRegex) and the JSON-schema generator (schemas/), so the
// editor schema and runtime validation cannot drift.
const ClusterNamePattern = `^[a-z][a-z0-9-]*[a-z0-9]$|^[a-z]$`

// clusterNameRegex matches DNS-1123 subdomain names: lowercase alphanumeric with optional hyphens.
// Must start with a letter, end with alphanumeric, and be at most 63 characters.
// See: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names
var clusterNameRegex = regexp.MustCompile(ClusterNamePattern)

// ClusterNameMaxLength is the maximum length for a cluster name.
const ClusterNameMaxLength = 63

// ValidateClusterName validates that a cluster name is DNS-1123 compliant.
// Cluster names are used in Docker container names, Kubernetes contexts, and YAML fields,
// which require DNS-1123 subdomain names (lowercase alphanumeric and dashes only).
//
// Returns nil if the name is valid, or an error describing the validation failure.
func ValidateClusterName(name string) error {
	if name == "" {
		return nil // Empty names are allowed (means use default)
	}

	if len(name) > ClusterNameMaxLength {
		return fmt.Errorf(
			"%w: %q exceeds max %d characters (got %d)",
			ErrClusterNameTooLong, name, ClusterNameMaxLength, len(name),
		)
	}

	if !clusterNameRegex.MatchString(name) {
		return fmt.Errorf(
			"%w: %q must be DNS-1123 compliant "+
				"(lowercase letters, numbers, and hyphens; must start with a letter; "+
				"must not end with a hyphen)",
			ErrClusterNameInvalid, name,
		)
	}

	return nil
}

// ValidateMirrorRegistriesForProvider validates that mirror registries are compatible with the provider.
// Cloud providers (like Hetzner) cannot access local Docker containers running as mirror registries.
// For cloud providers, mirror registries must point to external, internet-accessible registries.
//
// Parameters:
//   - provider: The infrastructure provider being used
//   - mirrorRegistries: List of mirror registry specifications
//
// Returns nil if valid, or an error if local mirrors are configured for a cloud provider.
func ValidateMirrorRegistriesForProvider(provider Provider, mirrorRegistries []string) error {
	if len(mirrorRegistries) == 0 {
		return nil
	}

	// Cloud providers cannot access local Docker containers as mirrors
	if provider.IsCloud() {
		for _, spec := range mirrorRegistries {
			if isLocalMirrorSpec(spec) {
				return fmt.Errorf(
					"%w: %q references a local endpoint; "+
						"cloud-based clusters cannot access local Docker containers; "+
						"use external registry URLs instead (e.g., docker.io=https://registry-1.docker.io)",
					ErrMirrorRegistryNotSupported,
					spec,
				)
			}
		}
	}

	return nil
}

// isLocalMirrorSpec checks if a mirror specification references a local endpoint.
// Local endpoints include localhost, 127.0.0.1, 0.0.0.0, and host.docker.internal.
func isLocalMirrorSpec(spec string) bool {
	// Normalize to lowercase for comparison
	lower := strings.ToLower(spec)

	// Check for local patterns in the spec
	// These patterns indicate local Docker container references that won't work on cloud providers
	localPatterns := []string{
		localhostHost,
		loopbackIP,
		"0.0.0.0",
		"host.docker.internal",
		"[::1]", // IPv6 localhost
	}

	for _, pattern := range localPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// ValidateLocalRegistryForProvider validates that local registry configuration is compatible with the provider.
// Cloud providers (like Hetzner) cannot use local Docker registries and must use external registries.
//
// Parameters:
//   - provider: The infrastructure provider being used
//   - registry: The local registry configuration
//
// Returns nil if valid, or an error if local registry is enabled without external host for cloud provider.
func ValidateLocalRegistryForProvider(provider Provider, registry LocalRegistry) error {
	if !registry.Enabled() {
		return nil
	}

	// Cloud and Kubernetes providers require external registries with proper host configuration
	if provider.IsCloud() || provider == ProviderKubernetes {
		if !registry.IsExternal() {
			return ErrLocalRegistryNotSupported
		}
	}

	return nil
}

// poolNameRegex matches DNS-1123 label names: lowercase alphanumeric with optional hyphens.
// Must start and end with a lowercase alphanumeric character, and be at most 63 characters.
var poolNameRegex = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// PoolNameMaxLength is the maximum length for a pool name (DNS-1123 label limit).
const PoolNameMaxLength = 63

// validateAutoscalerEnumFields validates enum-typed fields on the autoscaler configuration.
// It returns an error if any field carries a value that is not in its valid set.
func validateAutoscalerEnumFields(
	autoscaler *NodeAutoscalerConfig,
	pod *PodAutoscalerConfig,
) error {
	expandersErr := validateAutoscalerExpanders(autoscaler.Expander)
	if expandersErr != nil {
		return expandersErr
	}

	if pod.Horizontal != "" && !slices.Contains(ValidPodAutoscalerHorizontals(), pod.Horizontal) {
		return fmt.Errorf("%w: %q", ErrInvalidPodAutoscalerHorizontal, pod.Horizontal)
	}

	if pod.Vertical != "" && !slices.Contains(ValidPodAutoscalerVerticals(), pod.Vertical) {
		return fmt.Errorf("%w: %q", ErrInvalidPodAutoscalerVertical, pod.Vertical)
	}

	return nil
}

// validateAutoscalerExpanders validates an autoscaler expander priority list.
// Each entry must be a known strategy, and no strategy may appear more than once.
// An empty list is valid (the installer falls back to the default expander).
func validateAutoscalerExpanders(expanders AutoscalerExpanderList) error {
	seen := make(map[AutoscalerExpander]struct{}, len(expanders))

	for _, expander := range expanders {
		if !slices.Contains(ValidAutoscalerExpanders(), expander) {
			return fmt.Errorf("%w: %q", ErrInvalidAutoscalerExpander, expander)
		}

		if _, exists := seen[expander]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicateAutoscalerExpander, expander)
		}

		seen[expander] = struct{}{}
	}

	return nil
}

// validateExpanderForProvider checks that every chosen autoscaler expander
// strategy is supported by the cluster's infrastructure provider. The Hetzner
// cloud provider in the upstream cluster-autoscaler does not implement the
// pricing API, so the Price expander causes a fatal crash on startup — whether it
// is the sole expander or one entry in a priority list.
func validateExpanderForProvider(
	provider Provider,
	autoscaler *NodeAutoscalerConfig,
) error {
	if !autoscaler.Enabled.IsEnabled() || len(autoscaler.Expander) == 0 {
		return nil
	}

	if provider == ProviderHetzner &&
		slices.Contains(autoscaler.Expander, AutoscalerExpanderPrice) {
		return fmt.Errorf(
			"%w: %q is not supported on %s (Hetzner does not implement the pricing API); "+
				"use %s or %s instead",
			ErrExpanderNotSupportedForProvider,
			AutoscalerExpanderPrice,
			ProviderHetzner,
			AutoscalerExpanderLeastWaste,
			AutoscalerExpanderRandom,
		)
	}

	return nil
}

// validateNodePools checks each NodePool for name validity, min ≤ max, valid
// labels/taints, and uniqueness.
func validateNodePools(pools []NodePool) error {
	seen := make(map[string]struct{}, len(pools))

	for idx, pool := range pools {
		fieldErr := validateNodePoolFields(pool, idx)
		if fieldErr != nil {
			return fieldErr
		}

		labelTaintErr := validatePoolLabelsAndTaints(pool)
		if labelTaintErr != nil {
			return labelTaintErr
		}

		if _, exists := seen[pool.Name]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicatePoolName, pool.Name)
		}

		seen[pool.Name] = struct{}{}
	}

	return nil
}

// validateNodePoolFields checks a single pool's name, server type, location, and
// capacity (min/max) fields.
func validateNodePoolFields(pool NodePool, idx int) error {
	if len(pool.Name) > PoolNameMaxLength {
		return fmt.Errorf(
			"%w: pool[%d] %q exceeds max %d characters (got %d)",
			ErrInvalidPoolName, idx, pool.Name, PoolNameMaxLength, len(pool.Name),
		)
	}

	if !poolNameRegex.MatchString(pool.Name) {
		return fmt.Errorf(
			"%w: pool[%d] %q must be a DNS-1123 label "+
				"(lowercase letters, numbers, and hyphens; "+
				"must start and end with a lowercase alphanumeric character)",
			ErrInvalidPoolName, idx, pool.Name,
		)
	}

	if pool.ServerType == "" {
		return fmt.Errorf("%w: pool[%d] %q", ErrPoolServerTypeEmpty, idx, pool.Name)
	}

	if pool.Location == "" {
		return fmt.Errorf("%w: pool[%d] %q", ErrPoolLocationEmpty, idx, pool.Name)
	}

	if pool.Min < 0 || pool.Max < 0 {
		return fmt.Errorf(
			"%w: pool %q has negative min=%d or max=%d",
			ErrInvalidPoolCapacity, pool.Name, pool.Min, pool.Max,
		)
	}

	if pool.Min > pool.Max {
		return fmt.Errorf(
			"%w: pool %q has min=%d > max=%d",
			ErrPoolMinExceedsMax, pool.Name, pool.Min, pool.Max,
		)
	}

	return nil
}

// validatePoolLabelsAndTaints checks that a pool's Kubernetes node labels and
// taints are well-formed: label keys/values and taint keys/values must satisfy
// the Kubernetes label syntax, and taint effects must be one of the supported
// effects.
func validatePoolLabelsAndTaints(pool NodePool) error {
	for key, value := range pool.Labels {
		if errs := validation.IsQualifiedName(key); len(errs) > 0 {
			return fmt.Errorf(
				"%w: pool %q label key %q: %s",
				ErrInvalidPoolLabel, pool.Name, key, strings.Join(errs, "; "),
			)
		}

		if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
			return fmt.Errorf(
				"%w: pool %q label %q value %q: %s",
				ErrInvalidPoolLabel, pool.Name, key, value, strings.Join(errs, "; "),
			)
		}
	}

	for idx, taint := range pool.Taints {
		taintErr := validatePoolTaint(pool.Name, idx, taint)
		if taintErr != nil {
			return taintErr
		}
	}

	return nil
}

// validatePoolTaint validates a single pool taint's key, value, and effect.
func validatePoolTaint(poolName string, idx int, taint NodePoolTaint) error {
	if errs := validation.IsQualifiedName(taint.Key); len(errs) > 0 {
		return fmt.Errorf(
			"%w: pool %q taint[%d] key %q: %s",
			ErrInvalidPoolTaint, poolName, idx, taint.Key, strings.Join(errs, "; "),
		)
	}

	// An empty taint value is valid (IsValidLabelValue accepts ""), so this runs
	// unconditionally.
	if errs := validation.IsValidLabelValue(taint.Value); len(errs) > 0 {
		return fmt.Errorf(
			"%w: pool %q taint[%d] %q value %q: %s",
			ErrInvalidPoolTaint, poolName, idx, taint.Key, taint.Value, strings.Join(errs, "; "),
		)
	}

	if !slices.Contains(ValidTaintEffects(), taint.Effect) {
		return fmt.Errorf(
			"%w: pool %q taint[%d] %q has invalid effect %q (valid: %s, %s, %s)",
			ErrInvalidPoolTaint, poolName, idx, taint.Key, taint.Effect,
			TaintEffectNoSchedule, TaintEffectPreferNoSchedule, TaintEffectNoExecute,
		)
	}

	return nil
}

// ValidateAutoscalerConfig validates the autoscaler configuration within a ClusterSpec.
// It checks pool-level constraints (name validity, min ≤ max, uniqueness) and, when
// targeting Hetzner with node autoscaling enabled, enforces that the total server
// capacity does not exceed the configured ServerLimit.
//
// Parameters:
//   - cluster: The ClusterSpec containing autoscaler and node-count configuration.
//   - provider: The ProviderSpec containing Hetzner-specific options (ServerLimit).
//
// Returns the first validation error encountered, or nil if the configuration is valid.
func ValidateAutoscalerConfig(
	cluster *ClusterSpec,
	provider *ProviderSpec,
) error {
	if cluster == nil {
		return nil
	}

	autoscaler := &cluster.Autoscaler.Node

	enumErr := validateAutoscalerEnumFields(autoscaler, &cluster.Autoscaler.Pod)
	if enumErr != nil {
		return enumErr
	}

	expanderErr := validateExpanderForProvider(cluster.Provider, autoscaler)
	if expanderErr != nil {
		return expanderErr
	}

	if autoscaler.Enabled.IsEnabled() && len(autoscaler.Pools) == 0 {
		return ErrAutoscalerEnabledNoPools
	}

	thresholdErr := validateScaleDownUtilizationThreshold(autoscaler)
	if thresholdErr != nil {
		return thresholdErr
	}

	err := validateNodePools(autoscaler.Pools)
	if err != nil {
		return err
	}

	return validateHetznerCapacity(cluster, provider, autoscaler)
}

// validateScaleDownUtilizationThreshold rejects a ScaleDownUtilizationThreshold
// that is not a decimal ratio in [0.0, 1.0]. An empty value is valid — it inherits
// the upstream cluster-autoscaler default (0.5). Validating at ksail.yaml apply time
// surfaces a typo (e.g. "abc" or "2.0") immediately instead of as a cluster-autoscaler
// crash-loop once the chart is installed.
func validateScaleDownUtilizationThreshold(autoscaler *NodeAutoscalerConfig) error {
	raw := autoscaler.ScaleDownUtilizationThreshold
	if raw == "" {
		return nil
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidScaleDownUtilizationThreshold, raw)
	}

	// NaN passes the range check below (NaN is neither < 0.0 nor > 1.0), so reject
	// it explicitly; (+/-)Inf is already caught by the bounds.
	if math.IsNaN(value) || value < 0.0 || value > 1.0 {
		return fmt.Errorf("%w: %q", ErrInvalidScaleDownUtilizationThreshold, raw)
	}

	return nil
}

// validateHetznerCapacity checks that the reachable total node count stays within
// serverLimit when provider is Hetzner and node autoscaling is enabled. The reachable
// total is controlPlanes+workers+poolCapacity, clamped by MaxNodesTotal (the
// cluster-wide --max-nodes-total ceiling) when that global cap is set.
func validateHetznerCapacity(
	cluster *ClusterSpec,
	provider *ProviderSpec,
	autoscaler *NodeAutoscalerConfig,
) error {
	if provider == nil ||
		cluster.Provider != ProviderHetzner ||
		!autoscaler.Enabled.IsEnabled() {
		return nil
	}

	if autoscaler.MaxNodesTotal < 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidMaxNodesTotal, autoscaler.MaxNodesTotal)
	}

	serverLimit, err := resolveServerLimit(provider.Hetzner.ServerLimit)
	if err != nil {
		return err
	}

	var poolCapacity int32
	for _, pool := range autoscaler.Pools {
		poolCapacity += pool.Max
	}

	// MaxNodesTotal is the cluster-wide node ceiling (control-planes + workers +
	// autoscaler nodes), passed verbatim to the cluster-autoscaler --max-nodes-total
	// flag, which the autoscaler evaluates against the count of ALL nodes. Without it
	// the cluster can grow to the static baseline plus the full pool capacity; with it,
	// growth stops at MaxNodesTotal. The reachable total must stay within serverLimit.
	//
	// The clamp never drops below the static baseline (control-planes + workers): those
	// nodes are provisioned unconditionally, independent of the autoscaler ceiling, so a
	// MaxNodesTotal smaller than the baseline must not hide a baseline that already
	// exceeds serverLimit.
	baseline := cluster.ControlPlanes + cluster.Workers

	reachableTotal := baseline + poolCapacity
	if autoscaler.MaxNodesTotal > 0 && autoscaler.MaxNodesTotal < reachableTotal {
		reachableTotal = max(autoscaler.MaxNodesTotal, baseline)
	}

	if reachableTotal > serverLimit {
		return fmt.Errorf(
			"%w: reachableTotal(%d) exceeds serverLimit(%d): controlPlanes(%d)+workers(%d)+poolCapacity(%d), maxNodesTotal(%d)", //nolint:lll
			ErrAutoscalerExceedsServerLimit,
			reachableTotal,
			serverLimit,
			cluster.ControlPlanes,
			cluster.Workers,
			poolCapacity,
			autoscaler.MaxNodesTotal,
		)
	}

	return validateSnapshotBuildSlot(
		cluster,
		autoscaler,
		snapshotSlotInput{
			reachableTotal: reachableTotal,
			serverLimit:    serverLimit,
			poolCapacity:   poolCapacity,
		},
	)
}

// snapshotSlotInput carries the capacity figures validateHetznerCapacity has already
// computed, so the snapshot-slot check reports them without recomputing.
type snapshotSlotInput struct {
	reachableTotal int32
	serverLimit    int32
	poolCapacity   int32
}

// snapshotBuildServerReserve is the number of Hetzner servers KSail must be able to create
// for its OWN work, on top of the cluster's own nodes.
//
// On the Talos + Hetzner path KSail builds the Talos snapshot the autoscaler boots its nodes
// from, and hcloud-upload-image does that by booting ONE temporary server. The deploy path
// ensures the autoscaler config secret on every deploy and builds the snapshot whenever one
// is absent, so a cluster whose reachable total can occupy every slot the account allows has
// nowhere to put that server.
//
// It fails late and confusingly: while the pinned Talos version's snapshot exists the lookup
// hits and nothing needs building, so the config looks fine for weeks. The next Talos version
// bump invalidates the lookup, every deploy must build, and each one dies mid-apply with
// hcloud's resource_limit_exceeded — recovery deploys included. Reserving the slot up front
// turns that runtime deadlock into a config error. See ksail#6171.
const snapshotBuildServerReserve int32 = 1

// validateSnapshotBuildSlot rejects a Talos + Hetzner autoscaler config that can grow into
// every available server slot, leaving none for the snapshot build's temporary server.
//
// Scoped to Talos deliberately: EnsureTalosSnapshot is called only from the Talos Hetzner
// provisioner, so the k3s and kubeadm Hetzner paths never build a snapshot and are entitled
// to the full limit.
func validateSnapshotBuildSlot(
	cluster *ClusterSpec,
	autoscaler *NodeAutoscalerConfig,
	capacity snapshotSlotInput,
) error {
	if cluster.Distribution != DistributionTalos {
		return nil
	}

	usableLimit := capacity.serverLimit - snapshotBuildServerReserve
	if capacity.reachableTotal <= usableLimit {
		return nil
	}

	return fmt.Errorf(
		"%w: reachableTotal(%d) leaves no slot free of serverLimit(%d); keep it at or below %d (serverLimit minus %d reserved for the snapshot build's temporary server), or raise serverLimit: controlPlanes(%d)+workers(%d)+poolCapacity(%d), maxNodesTotal(%d)", //nolint:lll
		ErrAutoscalerLeavesNoSnapshotSlot,
		capacity.reachableTotal,
		capacity.serverLimit,
		usableLimit,
		snapshotBuildServerReserve,
		cluster.ControlPlanes,
		cluster.Workers,
		capacity.poolCapacity,
		autoscaler.MaxNodesTotal,
	)
}

// resolveServerLimit validates and normalises the configured Hetzner server limit.
// A negative limit is rejected; zero falls back to DefaultHetznerServerLimit.
func resolveServerLimit(limit int32) (int32, error) {
	if limit < 0 {
		return 0, fmt.Errorf("%w: got %d", ErrInvalidServerLimit, limit)
	}

	if limit == 0 {
		return DefaultHetznerServerLimit, nil
	}

	return limit, nil
}

// ValidateAllowedCIDRs validates that each entry in allowedCIDRs is a valid CIDR block.
// Returns nil when the slice is empty (meaning default 0.0.0.0/0 and ::/0 behavior).
func ValidateAllowedCIDRs(cidrs []string) error {
	for idx, cidr := range cidrs {
		trimmed := strings.TrimSpace(cidr)
		if trimmed == "" {
			return fmt.Errorf("%w: entry[%d] must not be empty", ErrInvalidAllowedCIDR, idx)
		}

		_, _, err := net.ParseCIDR(trimmed)
		if err != nil {
			return fmt.Errorf("%w: entry[%d] %q: %w", ErrInvalidAllowedCIDR, idx, cidr, err)
		}
	}

	return nil
}

// ValidateOIDCConfig validates the OIDC authentication configuration.
// When OIDC is enabled (IssuerURL is set), ClientID must also be set.
// IssuerURL must use HTTPS. ClientID alone without IssuerURL is invalid.
func ValidateOIDCConfig(oidc *OIDCSpec) error {
	if oidc == nil {
		return nil
	}

	// ClientID without IssuerURL is a partial/invalid configuration
	if oidc.ClientID != "" && oidc.IssuerURL == "" {
		return fmt.Errorf("%w: issuerURL is required when clientID is set", ErrInvalidOIDCConfig)
	}

	if !oidc.Enabled() {
		return nil
	}

	if oidc.ClientID == "" {
		return fmt.Errorf("%w: clientID is required when issuerURL is set", ErrInvalidOIDCConfig)
	}

	if !strings.HasPrefix(oidc.IssuerURL, "https://") {
		return fmt.Errorf("%w: issuerURL must use HTTPS scheme", ErrInvalidOIDCConfig)
	}

	// Normalize: trim whitespace, reject empty, and deduplicate scopes.
	seen := make(map[string]struct{}, len(oidc.ExtraScopes))
	normalized := make([]string, 0, len(oidc.ExtraScopes))

	for idx, scope := range oidc.ExtraScopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			return fmt.Errorf("%w: extraScopes[%d] must not be empty", ErrInvalidOIDCConfig, idx)
		}

		if _, dup := seen[trimmed]; !dup {
			seen[trimmed] = struct{}{}
			normalized = append(normalized, trimmed)
		}
	}

	oidc.ExtraScopes = normalized

	return nil
}

// ValidateNestedCIDRs validates that the nested cluster's pod and service CIDRs
// do not overlap with common host cluster CIDR ranges.
// Returns nil if valid, or ErrCIDROverlap describing the conflict.
func ValidateNestedCIDRs(podCIDR, serviceCIDR string) error {
	// Common host cluster CIDR ranges to check against
	hostRanges := []string{
		"10.244.0.0/16", // common host pod CIDR (Flannel, Calico default)
		"10.96.0.0/12",  // common host service CIDR (kubeadm default)
	}

	for _, cidr := range []struct {
		name  string
		value string
	}{
		{"podCidr", podCIDR},
		{"serviceCidr", serviceCIDR},
	} {
		if cidr.value == "" {
			continue
		}

		_, nestedNet, err := net.ParseCIDR(cidr.value)
		if err != nil {
			return fmt.Errorf("invalid %s %q: %w", cidr.name, cidr.value, err)
		}

		for _, hostRange := range hostRanges {
			_, hostNet, _ := net.ParseCIDR(hostRange)

			if nestedNet.Contains(hostNet.IP) || hostNet.Contains(nestedNet.IP) {
				return fmt.Errorf(
					"%w: %s %q overlaps with common host range %s",
					ErrCIDROverlap,
					cidr.name,
					cidr.value,
					hostRange,
				)
			}
		}
	}

	return nil
}

// ErrInvalidEKSConfig indicates an invalid EKS options configuration.
var ErrInvalidEKSConfig = errors.New("invalid EKS configuration")

// ValidateEKSConfig validates the EKS distribution options before any
// provisioning happens: a deterministically-invalid value must fail here, at
// config load, rather than after a billable cluster has already been created
// (the installer re-checks as defense in depth).
func ValidateEKSConfig(cluster *ClusterSpec) error {
	if cluster == nil || cluster.Distribution != DistributionEKS {
		return nil
	}

	name := strings.TrimSpace(cluster.EKS.AWSLoadBalancerControllerServiceAccount)
	if name == "" {
		return nil
	}

	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return fmt.Errorf(
			"%w: awsLoadBalancerControllerServiceAccount %q must be a valid DNS-1123 subdomain: %s",
			ErrInvalidEKSConfig, name, strings.Join(errs, "; "),
		)
	}

	return nil
}

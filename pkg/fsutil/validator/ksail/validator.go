package ksail

import (
	"fmt"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/validator"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	k3dapi "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const requiredCiliumArgs = 2

// cniFieldPath is the ksail.yaml field path reported for CNI alignment errors.
const cniFieldPath = "spec.cluster.cni"

// Validator validates KSail cluster configurations for semantic correctness and cross-configuration consistency.
type Validator struct {
	kindConfig     *kindv1alpha4.Cluster
	k3dConfig      *k3dapi.SimpleConfig
	talosConfig    *talosconfigmanager.Configs
	vclusterConfig *clusterprovisioner.VClusterConfig
	kwokConfig     *clusterprovisioner.KWOKConfig
}

// NewValidator creates a new KSail configuration validator without distribution configuration.
// Use NewValidatorForKind or NewValidatorForK3d for distribution-specific validation.
func NewValidator() *Validator {
	return &Validator{}
}

// NewValidatorForKind creates a new KSail configuration validator with Kind distribution configuration.
// The Kind config is used for cross-configuration validation (name consistency, CNI alignment).
func NewValidatorForKind(kindConfig *kindv1alpha4.Cluster) *Validator {
	return &Validator{
		kindConfig: kindConfig,
	}
}

// NewValidatorForK3d creates a new KSail configuration validator with K3d distribution configuration.
// The K3d config is used for cross-configuration validation (name consistency, CNI alignment).
func NewValidatorForK3d(k3dConfig *k3dapi.SimpleConfig) *Validator {
	return &Validator{
		k3dConfig: k3dConfig,
	}
}

// NewValidatorForTalos creates a new KSail configuration validator with Talos distribution configuration.
// The Talos config is used for cross-configuration validation (CNI alignment).
func NewValidatorForTalos(talosConfig *talosconfigmanager.Configs) *Validator {
	return &Validator{
		talosConfig: talosConfig,
	}
}

// NewValidatorForVCluster creates a new KSail configuration validator with VCluster distribution configuration.
// The VCluster config is used for cross-configuration validation (name consistency).
func NewValidatorForVCluster(vclusterConfig *clusterprovisioner.VClusterConfig) *Validator {
	return &Validator{
		vclusterConfig: vclusterConfig,
	}
}

// NewValidatorForKWOK creates a new KSail configuration validator with KWOK distribution configuration.
// The KWOK config is used for cross-configuration validation (name consistency).
func NewValidatorForKWOK(kwokConfig *clusterprovisioner.KWOKConfig) *Validator {
	return &Validator{
		kwokConfig: kwokConfig,
	}
}

// Validate performs validation on a loaded KSail cluster configuration.
func (v *Validator) Validate(config *v1alpha1.Cluster) *validator.ValidationResult {
	result := validator.NewValidationResult("ksail.yaml")

	// Handle nil config
	if config == nil {
		result.AddError(validator.ValidationError{
			Field:         "config",
			Message:       "configuration is nil",
			FixSuggestion: "Provide a valid KSail cluster configuration",
		})

		return result
	}

	// Validate required metadata fields
	validator.ValidateMetadata(
		config.Kind,
		config.APIVersion,
		"Cluster",
		"ksail.io/v1alpha1",
		result,
	)

	// Validate distribution field
	v.validateDistribution(config, result)
	v.validateGitOpsEngine(config, result)

	// Perform cross-configuration validation
	v.validateContextName(config, result)

	// Validate CNI alignment with distribution config
	v.validateCNIAlignment(config, result)
	v.validateRegistry(config, result)
	v.validateFlux(config, result)
	v.validateAutoscalerConfig(config, result)
	v.validatePublicNet(config, result)

	return result
}

// validateContextName warns when the context name does not match the pattern derived
// from the distribution and cluster name. A non-matching context is treated as a
// deliberate override rather than an error, since the only "valid" value is one KSail
// can compute itself. Only checked when a distribution config is provided to the validator.
func (v *Validator) validateContextName(
	config *v1alpha1.Cluster,
	result *validator.ValidationResult,
) {
	if config.Spec.Cluster.Connection.Context == "" {
		// Context is optional, no validation needed if empty
		return
	}

	expectedContext := v.getExpectedContextName(config)
	if expectedContext == "" {
		// For unknown distributions, or when no distribution config is provided, skip context validation
		return
	}

	if config.Spec.Cluster.Connection.Context != expectedContext {
		result.AddWarning(validator.ValidationError{
			Field:         "spec.cluster.connection.context",
			Message:       "context name does not match expected pattern for distribution",
			CurrentValue:  config.Spec.Cluster.Connection.Context,
			ExpectedValue: expectedContext,
			FixSuggestion: fmt.Sprintf(
				"Set context to '%s' to match the %s distribution pattern",
				expectedContext,
				config.Spec.Cluster.Distribution,
			),
		})
	}
}

// validateDistribution validates the distribution field for emptiness and validity.
func (v *Validator) validateDistribution(
	config *v1alpha1.Cluster,
	result *validator.ValidationResult,
) {
	distribution := config.Spec.Cluster.Distribution

	// Check if distribution is empty or invalid
	if distribution == "" || !distribution.IsValid() {
		var message, fixSuggestion string

		validValues := strings.Join(distribution.ValidValues(), ", ")

		if distribution == "" {
			message = "distribution is required"
			fixSuggestion = "Set spec.cluster.distribution to a supported distribution type"
		} else {
			message = "invalid distribution value"
			fixSuggestion = "Use a supported distribution: " + validValues
		}

		result.AddError(validator.ValidationError{
			Field:         "spec.cluster.distribution",
			Message:       message,
			CurrentValue:  distribution,
			ExpectedValue: "one of: " + validValues,
			FixSuggestion: fixSuggestion,
		})
	}

	// Validate distributionConfig field
	if config.Spec.Cluster.DistributionConfig == "" {
		result.AddError(validator.ValidationError{
			Field:         "spec.cluster.distributionConfig",
			Message:       "distributionConfig is required",
			FixSuggestion: "Set spec.cluster.distributionConfig to the distribution configuration file path",
		})
	}
}

// getExpectedContextName returns the expected kubeconfig context name for the given
// configuration based on the distribution-specific naming conventions:
//   - Vanilla:  "kind-<cluster-name>"
//   - K3s:      "k3d-<cluster-name>"
//   - Talos:    "admin@<cluster-name>"
//   - VCluster: "vcluster-docker_<cluster-name>"
//
// The cluster name is derived from the distribution config. If no distribution config
// is available, an empty string is returned and context validation is skipped.
//
// For Talos with the Omni provider, context validation is skipped because Omni generates
// its own context naming scheme (e.g., "devantler-prod") that does not follow the
// "admin@<cluster-name>" convention used by locally provisioned Talos clusters.
func (v *Validator) getExpectedContextName(config *v1alpha1.Cluster) string {
	// Omni generates its own kubeconfig context names; skip validation
	if config.Spec.Cluster.Distribution == v1alpha1.DistributionTalos &&
		config.Spec.Cluster.Provider == v1alpha1.ProviderOmni {
		return ""
	}

	distributionName := v.getDistributionConfigName(config.Spec.Cluster.Distribution)
	if distributionName == "" {
		// No distribution config available, skip context validation
		return ""
	}

	return formatExpectedContextName(config.Spec.Cluster.Distribution, distributionName)
}

// formatExpectedContextName returns the canonical kubeconfig context name used
// by a given distribution's tooling, delegating to the single source in pkg/apis.
func formatExpectedContextName(
	distribution v1alpha1.Distribution,
	distributionName string,
) string {
	return distribution.ContextName(distributionName)
}

// getDistributionConfigName extracts the cluster name from the distribution configuration.
func (v *Validator) getDistributionConfigName(distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return v.getKindConfigName()
	case v1alpha1.DistributionK3s:
		return v.getK3dConfigName()
	case v1alpha1.DistributionTalos:
		return v.getTalosConfigName()
	case v1alpha1.DistributionVCluster:
		return v.getVClusterConfigName()
	case v1alpha1.DistributionKWOK:
		return v.getKWOKConfigName()
	case v1alpha1.DistributionEKS:
		// EKS config is not provided to the validator (eksctl manages it);
		// skip distribution-level name validation.
		return ""
	default:
		return ""
	}
}

// getKindConfigName returns the Kind configuration name if available.
// Returns empty string if no Kind config is provided to the validator.
func (v *Validator) getKindConfigName() string {
	if v.kindConfig != nil && v.kindConfig.Name != "" {
		return v.kindConfig.Name
	}

	// No Kind config provided, return empty to skip validation
	return ""
}

// getK3dConfigName returns the K3d configuration name if available.
// Returns empty string if no K3d config is provided to the validator.
func (v *Validator) getK3dConfigName() string {
	if v.k3dConfig != nil && v.k3dConfig.Name != "" {
		return v.k3dConfig.Name
	}

	// No K3d config provided, return empty to skip validation
	return ""
}

// getTalosConfigName returns the Talos configuration cluster name if available.
// Returns empty string if no Talos config is provided to the validator.
func (v *Validator) getTalosConfigName() string {
	if v.talosConfig != nil {
		return v.talosConfig.GetClusterName()
	}

	// No Talos config provided, return empty to skip validation
	return ""
}

// getVClusterConfigName returns the VCluster configuration cluster name if available.
// Returns empty string if no VCluster config is provided to the validator.
func (v *Validator) getVClusterConfigName() string {
	if v.vclusterConfig != nil && v.vclusterConfig.Name != "" {
		return v.vclusterConfig.Name
	}

	// No VCluster config provided, return empty to skip validation
	return ""
}

// getKWOKConfigName returns the KWOK configuration cluster name if available.
// Returns empty string if no KWOK config is provided to the validator.
func (v *Validator) getKWOKConfigName() string {
	if v.kwokConfig != nil && v.kwokConfig.Name != "" {
		return v.kwokConfig.Name
	}

	return ""
}

// validateCNIAlignment validates that the distribution configuration aligns with the CNI setting.
// When Cilium CNI is requested, the distribution config must have CNI disabled.
// When Default CNI is used, the distribution config must NOT have CNI disabled.
func (v *Validator) validateCNIAlignment(
	config *v1alpha1.Cluster,
	result *validator.ValidationResult,
) {
	cni := config.Spec.Cluster.CNI
	dist := config.Spec.Cluster.Distribution

	switch cni {
	case v1alpha1.CNICilium:
		v.validateCiliumCNI(dist, result)
	case v1alpha1.CNICalico:
		// Calico CNI alignment validation not yet implemented.
	case "", v1alpha1.CNIDefault:
		v.validateDefaultCNI(dist, result)
	}
}

// validateCiliumCNI checks that the distribution config has CNI disabled when Cilium is requested.
func (v *Validator) validateCiliumCNI(
	dist v1alpha1.Distribution,
	result *validator.ValidationResult,
) {
	switch dist {
	case v1alpha1.DistributionVanilla:
		v.validateKindCiliumCNIAlignment(result)
	case v1alpha1.DistributionK3s:
		v.validateK3dCiliumCNIAlignment(result)
	case v1alpha1.DistributionTalos:
		v.validateTalosCiliumCNIAlignment(result)
	case v1alpha1.DistributionVCluster, v1alpha1.DistributionKWOK, v1alpha1.DistributionEKS:
		// VCluster manages its own CNI internally; KWOK simulates all pods;
		// EKS uses AWS VPC CNI (managed by the cloud). No alignment check needed.
	}
}

// validateDefaultCNI checks that the distribution config does NOT have CNI disabled when Default CNI is used.
func (v *Validator) validateDefaultCNI(
	dist v1alpha1.Distribution,
	result *validator.ValidationResult,
) {
	switch dist {
	case v1alpha1.DistributionVanilla:
		v.validateKindDefaultCNIAlignment(result)
	case v1alpha1.DistributionK3s:
		v.validateK3dDefaultCNIAlignment(result)
	case v1alpha1.DistributionTalos:
		v.validateTalosDefaultCNIAlignment(result)
	case v1alpha1.DistributionVCluster, v1alpha1.DistributionKWOK, v1alpha1.DistributionEKS:
		// VCluster, KWOK, and EKS manage CNI internally; no alignment check needed.
	}
}

// validateKindCiliumCNIAlignment validates that Kind configuration has CNI disabled when Cilium is requested.
func (v *Validator) validateKindCiliumCNIAlignment(result *validator.ValidationResult) {
	if v.kindConfig == nil {
		// No Kind config provided for validation, skip
		return
	}

	if !v.kindConfig.Networking.DisableDefaultCNI {
		result.AddError(validator.ValidationError{
			Field:         cniFieldPath,
			Message:       "Cilium CNI requires disableDefaultCNI to be true in Kind configuration",
			FixSuggestion: "Add 'networking.disableDefaultCNI: true' to your kind.yaml configuration file",
		})
	}
}

// validateKindDefaultCNIAlignment validates that Kind configuration does NOT have CNI disabled when Default is used.
func (v *Validator) validateKindDefaultCNIAlignment(result *validator.ValidationResult) {
	if v.kindConfig == nil {
		// No Kind config provided for validation, skip
		return
	}

	if v.kindConfig.Networking.DisableDefaultCNI {
		result.AddError(validator.ValidationError{
			Field:         cniFieldPath,
			Message:       "Default CNI requires disableDefaultCNI to be false in Kind configuration",
			CurrentValue:  "disableDefaultCNI: true",
			ExpectedValue: "disableDefaultCNI: false (or omit the field)",
			FixSuggestion: "Remove 'networking.disableDefaultCNI: true' from your kind.yaml " +
				"configuration file or set CNI to Cilium",
		})
	}
}

// checkK3dFlannelAndNetworkPolicyStatus checks if Flannel and network policy are disabled in K3d configuration.
// Returns (hasFlannelDisabled, hasNetworkPolicyDisabled).
func (v *Validator) checkK3dFlannelAndNetworkPolicyStatus() (bool, bool) {
	var (
		hasFlannelDisabled       bool
		hasNetworkPolicyDisabled bool
	)

	for _, arg := range v.k3dConfig.Options.K3sOptions.ExtraArgs {
		switch arg.Arg {
		case "--flannel-backend=none":
			hasFlannelDisabled = true
		case "--disable-network-policy":
			hasNetworkPolicyDisabled = true
		}
	}

	return hasFlannelDisabled, hasNetworkPolicyDisabled
}

// validateK3dCiliumCNIAlignment validates that K3d configuration has Flannel disabled when Cilium is requested.
func (v *Validator) validateK3dCiliumCNIAlignment(result *validator.ValidationResult) {
	if v.k3dConfig == nil {
		// No K3d config provided for validation, skip
		return
	}

	hasFlannelDisabled, hasNetworkPolicyDisabled := v.checkK3dFlannelAndNetworkPolicyStatus()

	missingArgs := make([]string, 0, requiredCiliumArgs)
	if !hasFlannelDisabled {
		missingArgs = append(missingArgs, "'--flannel-backend=none'")
	}

	if !hasNetworkPolicyDisabled {
		missingArgs = append(missingArgs, "'--disable-network-policy'")
	}

	if len(missingArgs) == 0 {
		return
	}

	result.AddError(validator.ValidationError{
		Field: cniFieldPath,
		Message: fmt.Sprintf(
			"Cilium CNI requires %s in K3d configuration",
			strings.Join(missingArgs, " and "),
		),
		FixSuggestion: fmt.Sprintf(
			"Add %s to the K3s extra args in your k3d.yaml configuration file",
			strings.Join(missingArgs, " and "),
		),
	})
}

// validateK3dDefaultCNIAlignment validates that K3d configuration does NOT have Flannel disabled when Default is used.
func (v *Validator) validateK3dDefaultCNIAlignment(result *validator.ValidationResult) {
	if v.k3dConfig == nil {
		// No K3d config provided for validation, skip
		return
	}

	hasFlannelDisabled, hasNetworkPolicyDisabled := v.checkK3dFlannelAndNetworkPolicyStatus()

	problematicArgs := make([]string, 0, requiredCiliumArgs)
	if hasFlannelDisabled {
		problematicArgs = append(problematicArgs, "'--flannel-backend=none'")
	}

	if hasNetworkPolicyDisabled {
		problematicArgs = append(problematicArgs, "'--disable-network-policy'")
	}

	if len(problematicArgs) == 0 {
		return
	}

	result.AddError(validator.ValidationError{
		Field: cniFieldPath,
		Message: fmt.Sprintf(
			"Default CNI requires Flannel to be enabled, but found %s in K3d configuration",
			strings.Join(problematicArgs, " and "),
		),
		FixSuggestion: fmt.Sprintf(
			"Remove %s from the K3s extra args in your k3d.yaml configuration file or set CNI to Cilium",
			strings.Join(problematicArgs, " and "),
		),
	})
}

// validateTalosCiliumCNIAlignment validates that Talos configuration has default CNI disabled when Cilium is requested.
func (v *Validator) validateTalosCiliumCNIAlignment(result *validator.ValidationResult) {
	if v.talosConfig == nil {
		// No Talos config provided for validation, skip
		return
	}

	if !v.talosConfig.IsCNIDisabled() {
		result.AddError(validator.ValidationError{
			Field:   cniFieldPath,
			Message: "Cilium CNI requires cluster.network.cni.name to be 'none' in Talos configuration",
			FixSuggestion: "Add a disable-default-cni.yaml patch to your talos/cluster directory with " +
				"'cluster.network.cni.name: none', or run 'ksail cluster init --cni Cilium'",
		})
	}
}

// validateTalosDefaultCNIAlignment validates that Talos configuration has default CNI enabled
// when Default CNI is requested.
func (v *Validator) validateTalosDefaultCNIAlignment(result *validator.ValidationResult) {
	if v.talosConfig == nil {
		// No Talos config provided for validation, skip
		return
	}

	if v.talosConfig.IsCNIDisabled() {
		result.AddError(validator.ValidationError{
			Field:   cniFieldPath,
			Message: "Default CNI requires Flannel to be enabled, but Talos configuration has CNI disabled",
			FixSuggestion: "Remove the disable-default-cni.yaml patch from your talos/cluster directory, " +
				"or set CNI to Cilium in your ksail.yaml",
		})
	}
}

// validateGitOpsEngine ensures the GitOps engine value is supported.
func (v *Validator) validateGitOpsEngine(
	config *v1alpha1.Cluster,
	result *validator.ValidationResult,
) {
	if config.Spec.Cluster.GitOpsEngine == "" {
		return
	}

	engine := config.Spec.Cluster.GitOpsEngine

	validValueList := engine.ValidValues()
	if slices.Contains(validValueList, string(engine)) {
		return
	}

	validValues := strings.Join(validValueList, ", ")

	result.AddError(validator.ValidationError{
		Field:         "spec.cluster.gitOpsEngine",
		Message:       "invalid GitOps engine value",
		CurrentValue:  config.Spec.Cluster.GitOpsEngine,
		ExpectedValue: "one of: " + validValues,
		FixSuggestion: "Use a supported GitOps engine: " + validValues,
	})
}

// validateRegistry ensures registry settings are coherent.
func (v *Validator) validateRegistry(
	config *v1alpha1.Cluster,
	result *validator.ValidationResult,
) {
	if !config.Spec.Cluster.LocalRegistry.Enabled() {
		return
	}

	// For external registries, port 0 is valid (HTTPS with implicit port 443)
	if config.Spec.Cluster.LocalRegistry.IsExternal() {
		port := config.Spec.Cluster.LocalRegistry.ResolvedPort()
		// External registries can have port 0 (implicit HTTPS) or explicit port
		if port < 0 || port > 65535 {
			result.AddError(validator.ValidationError{
				Field:         "spec.cluster.localRegistry.registry",
				Message:       "registry port must be between 0 and 65535",
				CurrentValue:  port,
				ExpectedValue: "0-65535 (0 for HTTPS with implicit port)",
				FixSuggestion: "Use the registry host without port for HTTPS (e.g., ghcr.io/org/repo)",
			})
		}

		return
	}

	// For local registries, port must be explicitly set (1-65535)
	port := config.Spec.Cluster.LocalRegistry.ResolvedPort()
	if port <= 0 || port > 65535 {
		result.AddError(validator.ValidationError{
			Field:         "spec.cluster.localRegistry.registry",
			Message:       "registry port must be between 1 and 65535",
			CurrentValue:  port,
			ExpectedValue: "1-65535",
			FixSuggestion: "Specify a valid port in the registry spec (e.g., localhost:5050)",
		})
	}
}

// validateFlux ensures Flux-specific settings are valid when Flux is enabled.
func (v *Validator) validateFlux(
	_ *v1alpha1.Cluster,
	_ *validator.ValidationResult,
) {
	// Flux-specific configuration is now handled via the FluxInstance CR.
	// No additional validation required in the KSail config.
}

// validateAutoscalerConfig validates the autoscaler configuration, including
// node pool constraints (name validity, min ≤ max, uniqueness) and the Hetzner
// server limit guard when applicable.
func (v *Validator) validateAutoscalerConfig(
	config *v1alpha1.Cluster,
	result *validator.ValidationResult,
) {
	err := v1alpha1.ValidateAutoscalerConfig(&config.Spec.Cluster, &config.Spec.Provider)
	if err != nil {
		result.AddError(validator.ValidationError{
			Field:         "spec.cluster.autoscaler",
			Message:       err.Error(),
			FixSuggestion: "Review spec.cluster.autoscaler.node configuration",
		})
	}
}

// validatePublicNet warns when a Hetzner role is left with no public networking.
// There is no config-time error to raise: KSail always provisions and attaches a
// private network, so a node can never end up with neither a public IP nor a private
// network. Whether KSail can actually reach an IPv4-less node over that private
// network is validated at provisioning time (see diagnoseUnreachableNode in the Talos
// provisioner), since it depends on the runtime environment, not the config.
func (v *Validator) validatePublicNet(
	config *v1alpha1.Cluster,
	result *validator.ValidationResult,
) {
	if config.Spec.Cluster.Provider != v1alpha1.ProviderHetzner {
		return
	}

	hetzner := &config.Spec.Provider.Hetzner

	v.warnFullyPrivateRole(
		"worker",
		!hetzner.WorkerIPv4Enabled() && !hetzner.WorkerIPv6Enabled(),
		result,
	)
	v.warnFullyPrivateRole(
		"control-plane",
		!hetzner.ControlPlaneIPv4Enabled() && !hetzner.ControlPlaneIPv6Enabled(),
		result,
	)
}

// warnFullyPrivateRole adds a warning when a Hetzner node role has no public IPv4 or
// IPv6. Such nodes only work when the private network provides egress (a NAT gateway)
// and KSail can reach the node's Talos API over the private network.
func (v *Validator) warnFullyPrivateRole(
	role string,
	fullyPrivate bool,
	result *validator.ValidationResult,
) {
	if !fullyPrivate {
		return
	}

	result.AddWarning(validator.ValidationError{
		Field: "spec.provider.hetzner",
		Message: role + " nodes have no public IPv4 or IPv6; they require a NAT gateway " +
			"on the private network for egress (image pulls, Hetzner API, cluster join), " +
			"and KSail must run with private-network reachability to manage them",
		FixSuggestion: "Provision a NAT gateway on the private network, or enable a public IP family",
	})
}

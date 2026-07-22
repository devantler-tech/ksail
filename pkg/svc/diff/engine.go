package diff

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Engine computes configuration differences and classifies their impact.
type Engine struct {
	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
	// rules is the pre-computed scalar field rule table. It is built once in
	// NewEngine and reused across ComputeDiff calls to avoid per-call slice and
	// closure allocations.
	rules []fieldRule
}

// NewEngine creates a new diff engine for the given distribution and provider.
func NewEngine(distribution v1alpha1.Distribution, provider v1alpha1.Provider) *Engine {
	engine := &Engine{
		distribution: distribution,
		provider:     provider,
	}
	engine.rules = engine.scalarFieldRules()

	return engine
}

// ComputeDiff compares old and new ClusterSpec and ProviderSpec, and categorizes all changes.
func (e *Engine) ComputeDiff(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	oldProvider, newProvider *v1alpha1.ProviderSpec,
) *clusterupdate.UpdateResult {
	result := clusterupdate.NewEmptyUpdateResult()

	if oldSpec == nil || newSpec == nil {
		return result
	}

	// e.rules is always populated by NewEngine, the sole constructor.
	e.applyFieldRules(oldSpec, newSpec, result, e.rules)

	// Check complex / distribution-specific changes
	e.checkLocalRegistryChange(oldSpec, newSpec, result)
	e.checkVanillaOptionsChange(oldSpec, newSpec, result)
	e.checkTalosOptionsChange(oldSpec, newSpec, result)
	e.checkHetznerOptionsChange(oldSpec, oldProvider, newProvider, result)
	e.checkAutoscalerOptionsChange(oldSpec, newSpec, result)

	return result
}

// CheckWorkloadTag compares the currently deployed GitOps sync ref against the desired
// workload tag and appends an in-place change when they differ. This detects
// stale sync refs left by pre-v6.7.1 cluster creation.
// The caller is responsible for querying the cluster for oldTag and resolving
// newTag from the configuration.
func (e *Engine) CheckWorkloadTag(
	oldTag, newTag string,
	gitOpsEngine v1alpha1.GitOpsEngine,
	result *clusterupdate.UpdateResult,
) {
	if gitOpsEngine.IsNone() {
		return
	}

	appendChange(result, "cluster.workload.tag",
		oldTag, newTag, "",
		"workload tag can be updated in-place on the GitOps sync resource",
		clusterupdate.ChangeCategoryInPlace)
}

// CheckFluxDistributionVersion compares the running FluxInstance's
// spec.distribution.version against the desired version (resolved from a
// repo-declared FluxInstance, spec.workload.flux.distributionVersion, or the
// default) and appends an in-place change when they differ. Only relevant for
// the Flux engine. The caller passes oldVersion=="" when the FluxInstance cannot
// be introspected (e.g. it does not exist yet), which suppresses the diff so a
// non-introspectable seed never produces a false-positive change.
func (e *Engine) CheckFluxDistributionVersion(
	oldVersion, newVersion string,
	gitOpsEngine v1alpha1.GitOpsEngine,
	result *clusterupdate.UpdateResult,
) {
	if gitOpsEngine != v1alpha1.GitOpsEngineFlux {
		return
	}

	// An empty baseline means the running version could not be introspected
	// (e.g. the FluxInstance does not exist yet); suppress the diff so a
	// non-introspectable seed never produces a false-positive change.
	if oldVersion == "" {
		return
	}

	appendChange(result, "cluster.workload.flux.distributionVersion",
		oldVersion, newVersion, "",
		"Flux distribution version can be updated in-place by re-asserting the FluxInstance",
		clusterupdate.ChangeCategoryInPlace)
}

// fieldRule describes how to diff a single scalar field.
type fieldRule struct {
	field    string
	category clusterupdate.ChangeCategory
	reason   string
	// getVal extracts the string representation of this field from a ClusterSpec.
	getVal func(*v1alpha1.ClusterSpec) string
	// defaultVal, when non-empty, is substituted for any zero/empty value returned
	// by getVal. This prevents false-positive diffs when a config field is unset
	// (zero value) but the cluster state contains the applied default.
	defaultVal string
	// categoryFn, when non-nil, overrides the static category field. Use this for
	// rules whose change impact varies by distribution/provider (e.g. CDI is
	// recreate-required on Kind but reboot-required on Talos).
	categoryFn func() clusterupdate.ChangeCategory
	// skipWhenNewEmpty, when true, skips the diff when the desired (new) value is
	// empty. Use this for fields that are optional in the user config — when the user
	// hasn't set the field, any detected current value should not produce a diff.
	skipWhenNewEmpty bool
	// skipWhenOldEmpty, when true, skips the diff when the baseline (old) value is
	// empty — i.e. the current value could not be introspected from the cluster and
	// no persisted state supplied it. Use this for boot-time settings (e.g. the
	// Talos ISO ID) that a running cluster cannot report: with no baseline, falling
	// back to defaultVal would fabricate a perpetual false-positive diff whenever the
	// user pins a non-default value. Mirrors checkLocalRegistryChange's handling,
	// where an empty old value means "unknown", not a concrete value to diff against.
	skipWhenOldEmpty bool
}

// scalarFieldRules returns the table of simple scalar field diff rules.
// These fields follow a uniform pattern: compare old vs new, emit a Change if different.
// For CSI, CDI, MetricsServer, and LoadBalancer the effective value is used so that
// semantically equivalent states (e.g. Default and Disabled on Vanilla) compare as equal.
//
//nolint:funlen // Table-driven rule definitions are clearer as a single cohesive list.
func (e *Engine) scalarFieldRules() []fieldRule {
	return []fieldRule{
		{
			field:    "cluster.distribution",
			category: clusterupdate.ChangeCategoryRecreateRequired,
			reason:   "changing the Kubernetes distribution requires cluster recreation",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Distribution.String() },
		},
		{
			field:    "cluster.provider",
			category: clusterupdate.ChangeCategoryRecreateRequired,
			reason:   "changing the infrastructure provider requires cluster recreation",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Provider.String() },
		},
		{
			field:      "cluster.cni",
			category:   clusterupdate.ChangeCategoryInPlace,
			reason:     "CNI can be switched via Helm upgrade/uninstall",
			getVal:     func(s *v1alpha1.ClusterSpec) string { return s.CNI.String() },
			defaultVal: string(v1alpha1.CNIDefault),
		},
		{
			field:    "cluster.csi",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "CSI can be switched via Helm install/uninstall",
			getVal: func(spec *v1alpha1.ClusterSpec) string {
				// Vanilla (Kind) and VCluster (Vind with k8s distro) always bundle
				// local-path-provisioner regardless of KSail's CSI configuration. The
				// detector reports CSIEnabled when the deployment exists, but it
				// cannot distinguish the distribution-bundled CSI from one installed
				// by KSail. To prevent false-positive diffs (e.g. config says
				// CSIDisabled while the bundled deployment is still present), skip
				// CSI comparison entirely by returning a constant for both sides.
				if e.distribution == v1alpha1.DistributionVanilla ||
					e.distribution == v1alpha1.DistributionVCluster {
					return string(v1alpha1.CSIEnabled)
				}

				return string(spec.CSI.EffectiveValue(e.distribution, e.provider))
			},
		},
		{
			field:  "cluster.cdi",
			reason: "CDI requires node-level containerd reconfiguration",
			categoryFn: func() clusterupdate.ChangeCategory {
				// Kind bakes containerd config at creation time — CDI changes
				// require cluster recreation. Talos can apply machine config
				// patches via reboot.
				if e.distribution == v1alpha1.DistributionVanilla {
					return clusterupdate.ChangeCategoryRecreateRequired
				}

				return clusterupdate.ChangeCategoryRebootRequired
			},
			getVal: func(spec *v1alpha1.ClusterSpec) string {
				// K3s, VCluster, KWOK, and the managed cloud distributions
				// (EKS, GKE, AKS) have no CDI runtime wiring — suppress diffs.
				switch e.distribution {
				case v1alpha1.DistributionK3s,
					v1alpha1.DistributionVCluster,
					v1alpha1.DistributionKWOK,
					v1alpha1.DistributionEKS,
					v1alpha1.DistributionGKE,
					v1alpha1.DistributionAKS:
					return string(v1alpha1.CDIDisabled)
				case v1alpha1.DistributionVanilla, v1alpha1.DistributionTalos:
					return string(spec.CDI.EffectiveValue(e.distribution, e.provider))
				}

				return string(spec.CDI.EffectiveValue(e.distribution, e.provider))
			},
		},
		{
			field:    "cluster.metricsServer",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "metrics-server can be installed via Helm; disabling requires cluster recreation",
			getVal: func(s *v1alpha1.ClusterSpec) string {
				return string(s.MetricsServer.EffectiveValue(e.distribution, e.provider))
			},
		},
		{
			field:    "cluster.loadBalancer",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "load balancer can be enabled/disabled via Helm",
			getVal: func(spec *v1alpha1.ClusterSpec) string {
				// VCluster delegates LoadBalancer to the host cluster; KSail does
				// not install or uninstall anything, so the setting has no effect.
				// Always return "Default" for both sides to prevent false-positive diffs.
				if e.distribution == v1alpha1.DistributionVCluster {
					return string(v1alpha1.LoadBalancerDefault)
				}

				return string(spec.LoadBalancer.EffectiveValue(e.distribution, e.provider))
			},
		},
		{
			field:    "cluster.eks.experimentalAWSLoadBalancerController",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "AWS Load Balancer Controller can be installed/uninstalled via Helm",
			getVal: func(spec *v1alpha1.ClusterSpec) string {
				if e.distribution != v1alpha1.DistributionEKS ||
					e.provider != v1alpha1.ProviderAWS {
					return strconv.FormatBool(false)
				}

				return strconv.FormatBool(spec.EKS.ExperimentalAWSLoadBalancerController)
			},
		},
		{
			field:      "cluster.certManager",
			category:   clusterupdate.ChangeCategoryInPlace,
			reason:     "cert-manager can be installed/uninstalled via Helm",
			getVal:     func(s *v1alpha1.ClusterSpec) string { return s.CertManager.String() },
			defaultVal: string(v1alpha1.CertManagerDisabled),
		},
		{
			field:      "cluster.policyEngine",
			category:   clusterupdate.ChangeCategoryInPlace,
			reason:     "policy engine can be switched via Helm install/uninstall",
			getVal:     func(s *v1alpha1.ClusterSpec) string { return s.PolicyEngine.String() },
			defaultVal: string(v1alpha1.PolicyEngineNone),
		},
		{
			field:      "cluster.gitOpsEngine",
			category:   clusterupdate.ChangeCategoryInPlace,
			reason:     "GitOps engine can be switched via Helm install/uninstall",
			getVal:     func(s *v1alpha1.ClusterSpec) string { return s.GitOpsEngine.String() },
			defaultVal: string(v1alpha1.GitOpsEngineNone),
		},
	}
}

// appendChange appends a single diff change to the appropriate category slice in result.
// Both applyFieldRules and applyProviderFieldRules delegate to this helper to
// avoid duplicating the default-value substitution and category dispatch logic.
//

func appendChange(
	result *clusterupdate.UpdateResult,
	field, oldVal, newVal, defaultVal, reason string,
	category clusterupdate.ChangeCategory,
) {
	if defaultVal != "" {
		if oldVal == "" {
			oldVal = defaultVal
		}

		if newVal == "" {
			newVal = defaultVal
		}
	}

	if oldVal == newVal {
		return
	}

	routeChange(result, clusterupdate.Change{
		Field:    field,
		OldValue: oldVal,
		NewValue: newVal,
		Category: category,
		Reason:   reason,
	})
}

// routeChange appends a fully-formed change to the result slice matching its
// category. Unlike appendChange it performs no equality check or default
// substitution, so callers that have already decided a change occurred (and may
// have transformed the displayed values, e.g. redacting secrets) can route it
// without the value-based short-circuit dropping the entry.
func routeChange(result *clusterupdate.UpdateResult, change clusterupdate.Change) {
	switch change.Category {
	case clusterupdate.ChangeCategoryRecreateRequired:
		result.RecreateRequired = append(result.RecreateRequired, change)
	case clusterupdate.ChangeCategoryInPlace:
		result.InPlaceChanges = append(result.InPlaceChanges, change)
	case clusterupdate.ChangeCategoryRebootRequired:
		result.RebootRequired = append(result.RebootRequired, change)
	case clusterupdate.ChangeCategoryWipeRequired:
		result.WipeRequired = append(result.WipeRequired, change)
	case clusterupdate.ChangeCategoryRollingRecreate:
		result.RollingRecreate = append(result.RollingRecreate, change)
	case clusterupdate.ChangeCategoryUnknown:
		// Unknown-baseline entries are produced by appendUnknownIfChanged, not
		// here; route defensively so the category is never silently dropped.
		result.UnknownBaseline = append(result.UnknownBaseline, change)
	}
}

// applyFieldRules evaluates each field rule and appends changes to the result.
func (e *Engine) applyFieldRules(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
	rules []fieldRule,
) {
	for _, rule := range rules {
		newVal := rule.getVal(newSpec)
		if rule.skipWhenNewEmpty && newVal == "" {
			continue
		}

		oldVal := rule.getVal(oldSpec)

		// The baseline could not be determined (not introspectable, no persisted
		// state). Skip rather than diff against defaultVal, which would fabricate a
		// false-positive change whenever the user pins a non-default value (e.g. the
		// Talos ISO in stateless CI). See skipWhenOldEmpty.
		if rule.skipWhenOldEmpty && oldVal == "" {
			continue
		}

		// The baseline value could not be read from the cluster. Surface the
		// field as Unknown instead of computing a confident diff against the
		// default value (which would mislead the user into reinstalling
		// components that may already be healthy).
		if oldVal == clusterupdate.UnknownBaselineValue {
			e.appendUnknownIfChanged(result, rule, newVal)

			continue
		}

		cat := rule.category
		if rule.categoryFn != nil {
			cat = rule.categoryFn()
		}

		appendChange(result, rule.field,
			oldVal, newVal,
			rule.defaultVal, rule.reason, cat)
	}
}

// appendUnknownIfChanged appends an Unknown-baseline entry for a field whose
// current value could not be read. To avoid noise, it only emits an entry when
// the field's *default* baseline differs from the desired value — i.e. exactly
// when a normal diff would have reported a change. Fields the user left at their
// defaults produce no entry, since they would not have shown a change either.
func (e *Engine) appendUnknownIfChanged(
	result *clusterupdate.UpdateResult,
	rule fieldRule,
	newVal string,
) {
	defaultOld := rule.getVal(clusterupdate.DefaultCurrentSpec(e.distribution, e.provider))

	// Mirror appendChange's default-value substitution so the comparison matches
	// what a normal diff would have evaluated.
	if rule.defaultVal != "" {
		if defaultOld == "" {
			defaultOld = rule.defaultVal
		}

		if newVal == "" {
			newVal = rule.defaultVal
		}
	}

	if defaultOld == newVal {
		return
	}

	result.UnknownBaseline = append(result.UnknownBaseline, clusterupdate.Change{
		Field:    rule.field,
		OldValue: clusterupdate.UnknownBaselineValue,
		NewValue: newVal,
		Category: clusterupdate.ChangeCategoryUnknown,
		Reason:   "current cluster state could not be read; baseline is unknown",
	})
}

// componentBaselineUnknown reports whether the spec's detector-derived component
// fields were marked unknown (see clusterupdate.MarkComponentsUnknown). Checking
// CNI is sufficient because the marker sets all component fields together.
func (e *Engine) componentBaselineUnknown(spec *v1alpha1.ClusterSpec) bool {
	return string(spec.CNI) == clusterupdate.UnknownBaselineValue
}

// reasonRegistryIndependentOfOS explains why VCluster, KWOK, and EKS can change their registry
// configuration in place: none of them manages the registry through the node OS, so no node-level
// reconfiguration or cluster recreate is required.
const reasonRegistryIndependentOfOS = "VCluster/KWOK/EKS manage registry independently of the node OS"

// localRegistryReasonMap maps each distribution to the reason and category for a local registry change.
// For Kind, registry changes require recreate (containerd config is baked in).
// For all other distributions, registry mirrors can be updated in-place.
// Defined as a package-level variable to avoid reallocating the map on each call.
//
//nolint:gochecknoglobals // Immutable lookup table; avoids per-call heap allocation.
var localRegistryReasonMap = map[v1alpha1.Distribution]struct {
	reason   string
	category clusterupdate.ChangeCategory
}{
	v1alpha1.DistributionVanilla: {
		reason:   "Kind requires cluster recreate to change containerd registry config",
		category: clusterupdate.ChangeCategoryRecreateRequired,
	},
	v1alpha1.DistributionTalos: {
		reason:   "Talos supports .machine.registries updates without reboot",
		category: clusterupdate.ChangeCategoryInPlace,
	},
	v1alpha1.DistributionK3s: {
		reason:   "K3d supports registries.yaml updates",
		category: clusterupdate.ChangeCategoryInPlace,
	},
	v1alpha1.DistributionVCluster: {
		reason:   reasonRegistryIndependentOfOS,
		category: clusterupdate.ChangeCategoryInPlace,
	},
	v1alpha1.DistributionKWOK: {
		reason:   reasonRegistryIndependentOfOS,
		category: clusterupdate.ChangeCategoryInPlace,
	},
	v1alpha1.DistributionEKS: {
		reason:   reasonRegistryIndependentOfOS,
		category: clusterupdate.ChangeCategoryInPlace,
	},
}

// checkLocalRegistryChange checks if local registry config has changed.
//
// When the old registry is empty, the comparison is skipped because the
// detector cannot introspect the running cluster's local registry
// configuration. An empty old value means "unknown", not "none".
//
// Limitation: this means a genuine change from no-registry to a configured
// registry (or vice versa) will be silently ignored until state persistence
// is implemented. State persistence will populate the old value correctly,
// enabling proper change detection for all transitions.
func (e *Engine) checkLocalRegistryChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) {
	if oldSpec.LocalRegistry.Registry == "" {
		return
	}

	reasonCategory, ok := localRegistryReasonMap[e.distribution]
	if !ok {
		return
	}

	// Compare on the credential-redacted specs. The persisted baseline stores the
	// registry with its password masked (see state.SaveClusterSpec) while the
	// desired spec is env-expanded to the live secret, so comparing raw values
	// would report a spurious change on every update. Redacting both first keeps
	// the comparison stable and keeps the password out of the diff table, JSON
	// output, and recreate/reboot warnings. This Change is display-only — apply
	// logic resolves credentials from the spec, never from the diff. A
	// credentials-only rotation is consequently not surfaced: the baseline no
	// longer carries the password to compare against.
	oldRegistry := v1alpha1.RedactRegistryCredentials(oldSpec.LocalRegistry.Registry)
	newRegistry := v1alpha1.RedactRegistryCredentials(newSpec.LocalRegistry.Registry)

	if oldRegistry == newRegistry {
		return
	}

	routeChange(result, clusterupdate.Change{
		Field:    "cluster.localRegistry.registry",
		OldValue: oldRegistry,
		NewValue: newRegistry,
		Category: reasonCategory.category,
		Reason:   reasonCategory.reason,
	})
}

// checkVanillaOptionsChange checks Vanilla (Kind) specific option changes.
func (e *Engine) checkVanillaOptionsChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) {
	if e.distribution != v1alpha1.DistributionVanilla {
		return
	}

	// MirrorsDir change requires recreate (containerd config is baked at creation)
	appendChange(result, "cluster.vanilla.mirrorsDir",
		oldSpec.Vanilla.MirrorsDir, newSpec.Vanilla.MirrorsDir,
		"", "Kind containerd mirror config is baked at cluster creation",
		clusterupdate.ChangeCategoryRecreateRequired)
}

// checkTalosOptionsChange checks Talos-specific option changes.
func (e *Engine) checkTalosOptionsChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) {
	if e.distribution != v1alpha1.DistributionTalos {
		return
	}

	e.applyFieldRules(oldSpec, newSpec, result, talosFieldRules)
}

// talosFieldRules is the table of Talos-specific scalar field diff rules.
// Defined as a package-level variable to avoid reallocating the slice on each ComputeDiff call.
//
//nolint:gochecknoglobals // Immutable field-rule table; avoids per-call heap allocation.
var talosFieldRules = []fieldRule{
	{
		field:    "cluster.talos.version",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "version pin change only affects future operations (image selection, upgrade cap)",
		getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Talos.Version },
		// skipWhenNewEmpty suppresses false-positive diffs when the user has not
		// pinned a version in ksail.yaml. The detected running version is
		// informational; absence of a desired version means "no constraint".
		skipWhenNewEmpty: true,
	},
	{
		// Top-level field (spec.cluster.kubernetesVersion); only the Talos
		// distribution honors it, so the rule lives here rather than in the
		// distribution-agnostic rules to avoid false diffs for Kind/K3d/EKS.
		field:    "cluster.kubernetesVersion",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "Kubernetes version change re-renders the machine config and is applied to nodes",
		// Normalise both sides to drop any "v" prefix so "v1.32.0" and "1.32.0"
		// (the introspected baseline form) compare equal.
		getVal: func(s *v1alpha1.ClusterSpec) string {
			return strings.TrimPrefix(strings.TrimSpace(s.KubernetesVersion), "v")
		},
		// skipWhenNewEmpty: when the user has not pinned a Kubernetes version, the
		// provisioner tracks the running version (no upgrade), so there is no
		// spec-level change to report regardless of the detected baseline.
		skipWhenNewEmpty: true,
	},
	{
		field:    "cluster.controlPlanes",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "Talos supports adding/removing control-plane nodes via provider",
		getVal:   func(s *v1alpha1.ClusterSpec) string { return strconv.Itoa(int(s.ControlPlanes)) },
	},
	{
		field:    "cluster.workers",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "Talos supports adding/removing worker nodes via provider",
		getVal:   func(s *v1alpha1.ClusterSpec) string { return strconv.Itoa(int(s.Workers)) },
	},
	talosISORule,
}

// talosISORule is the ISO field rule used by the Talos field rules.
// ISO is a boot-time setting (Hetzner Cloud ISO ID) that a running cluster cannot
// report, so the baseline is known only from persisted state. getVal returns ""
// when ISO is 0 (unset). The two normalisation mechanisms are complementary:
//   - skipWhenOldEmpty handles an unknown *baseline* (old == ""): the diff is
//     skipped entirely (before appendChange), so we never fabricate a change
//     against defaultVal when the user pins a non-default ISO in stateless CI.
//   - defaultVal handles an unset *desired* value (new == "") against a known
//     baseline: it normalises the desired side to DefaultTalosISO so an unpinned
//     config does not diff against a non-zero persisted baseline.
//
// A genuine ISO change is still reported when both sides are known and differ,
// and the desired ISO is always applied to newly provisioned nodes regardless.
//
//nolint:gochecknoglobals // Immutable field-rule; avoids per-call heap allocation.
var talosISORule = fieldRule{
	field:            "cluster.talos.iso",
	category:         clusterupdate.ChangeCategoryInPlace,
	reason:           "ISO change only affects newly provisioned nodes",
	skipWhenOldEmpty: true,
	defaultVal:       strconv.FormatInt(v1alpha1.DefaultTalosISO, 10),
	getVal: func(s *v1alpha1.ClusterSpec) string {
		if s.Talos.ISO == 0 {
			return ""
		}

		return strconv.FormatInt(s.Talos.ISO, 10)
	},
}

// checkHetznerOptionsChange checks Hetzner-specific option changes.
func (e *Engine) checkHetznerOptionsChange(
	oldSpec *v1alpha1.ClusterSpec,
	oldProvider, newProvider *v1alpha1.ProviderSpec,
	result *clusterupdate.UpdateResult,
) {
	if e.provider != v1alpha1.ProviderHetzner {
		return
	}

	if oldProvider == nil || newProvider == nil {
		return
	}

	e.applyProviderFieldRules(oldProvider, newProvider, result, hetznerFieldRules)
	e.checkHetznerServerTypeChanges(oldSpec, oldProvider, newProvider, result)
}

// checkHetznerServerTypeChanges classifies control-plane and worker server-type
// changes. Unlike the static hetznerFieldRules, their impact depends on the
// running node counts: control planes can be rolled one at a time only when the
// cluster has enough etcd-quorum redundancy, and workers are only rolled when
// they exist. See clusterupdate.ControlPlaneServerTypeChangeCategory and
// WorkerServerTypeChangeCategory.
func (e *Engine) checkHetznerServerTypeChanges(
	oldSpec *v1alpha1.ClusterSpec,
	oldProvider, newProvider *v1alpha1.ProviderSpec,
	result *clusterupdate.UpdateResult,
) {
	cpCategory := clusterupdate.ControlPlaneServerTypeChangeCategory(int(oldSpec.ControlPlanes))
	appendChange(result, "provider.hetzner.controlPlaneServerType",
		oldProvider.Hetzner.ControlPlaneServerType, newProvider.Hetzner.ControlPlaneServerType,
		v1alpha1.DefaultHetznerServerType,
		serverTypeChangeReason(RoleControlPlane, cpCategory), cpCategory)

	workerCategory := clusterupdate.WorkerServerTypeChangeCategory(int(oldSpec.Workers))
	appendChange(result, "provider.hetzner.workerServerType",
		oldProvider.Hetzner.WorkerServerType, newProvider.Hetzner.WorkerServerType,
		v1alpha1.DefaultHetznerServerType,
		serverTypeChangeReason(RoleWorker, workerCategory), workerCategory)
}

// Role identifiers used when describing server-type changes.
const (
	// RoleControlPlane identifies control-plane nodes.
	RoleControlPlane = "control-plane"
	// RoleWorker identifies worker nodes.
	RoleWorker = "worker"
)

// serverTypeChangeReason returns a human-readable explanation for a server-type
// change classified into the given category.
func serverTypeChangeReason(role string, category clusterupdate.ChangeCategory) string {
	switch category {
	case clusterupdate.ChangeCategoryRollingRecreate:
		return "existing " + role + " servers are replaced one at a time to apply the new VM type"
	case clusterupdate.ChangeCategoryRecreateRequired:
		return "control plane lacks etcd-quorum redundancy to roll (need at least " +
			strconv.Itoa(clusterupdate.MinControlPlanesForRollingReplace) +
			" control planes); recreation is required to change VM type"
	case clusterupdate.ChangeCategoryInPlace,
		clusterupdate.ChangeCategoryRebootRequired,
		clusterupdate.ChangeCategoryWipeRequired,
		clusterupdate.ChangeCategoryUnknown:
		return "new " + role + " servers use the new type; no existing nodes to replace"
	default:
		return "new " + role + " servers use the new type; no existing nodes to replace"
	}
}

// providerFieldRule describes how to diff a single scalar field from ProviderSpec.
type providerFieldRule struct {
	field      string
	category   clusterupdate.ChangeCategory
	reason     string
	getVal     func(*v1alpha1.ProviderSpec) string
	defaultVal string
}

// hetznerFieldRules is the table of Hetzner-specific scalar field diff rules.
// Defined as a package-level variable to avoid reallocating the slice on each ComputeDiff call.
//
//nolint:gochecknoglobals // Immutable field-rule table; avoids per-call heap allocation.
var hetznerFieldRules = []providerFieldRule{
	{
		field:      "provider.hetzner.location",
		category:   clusterupdate.ChangeCategoryRecreateRequired,
		reason:     "datacenter location cannot be changed for existing servers",
		getVal:     func(s *v1alpha1.ProviderSpec) string { return s.Hetzner.Location },
		defaultVal: v1alpha1.DefaultHetznerLocation,
	},
	{
		field:    "provider.hetzner.networkName",
		category: clusterupdate.ChangeCategoryRecreateRequired,
		reason:   "cannot migrate servers between networks",
		getVal:   func(s *v1alpha1.ProviderSpec) string { return s.Hetzner.NetworkName },
	},
	{
		field:      "provider.hetzner.networkCidr",
		category:   clusterupdate.ChangeCategoryRecreateRequired,
		reason:     "network CIDR change requires PKI regeneration",
		getVal:     func(s *v1alpha1.ProviderSpec) string { return s.Hetzner.NetworkCIDR },
		defaultVal: v1alpha1.DefaultHetznerNetworkCIDR,
	},
	{
		field:    "provider.hetzner.sshKeyName",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "SSH key change only affects newly provisioned servers",
		getVal:   func(s *v1alpha1.ProviderSpec) string { return s.Hetzner.SSHKeyName },
	},
	{
		field:      "provider.hetzner.ingressFirewall",
		category:   clusterupdate.ChangeCategoryInPlace,
		reason:     "ingress firewall config is applied via Talos machine config patches",
		getVal:     func(s *v1alpha1.ProviderSpec) string { return string(s.Hetzner.IngressFirewall) },
		defaultVal: string(v1alpha1.IngressFirewallEnabled),
	},
}

// applyProviderFieldRules evaluates each provider field rule and appends changes to the result.
func (e *Engine) applyProviderFieldRules(
	oldProvider, newProvider *v1alpha1.ProviderSpec,
	result *clusterupdate.UpdateResult,
	rules []providerFieldRule,
) {
	for _, rule := range rules {
		appendChange(result, rule.field,
			rule.getVal(oldProvider), rule.getVal(newProvider),
			rule.defaultVal, rule.reason, rule.category)
	}
}

// checkAutoscalerOptionsChange detects changes to autoscaler configuration and emits
// appropriate in-place diff entries. All autoscaler changes are in-place because they
// are applied via Helm chart upgrades or installs/uninstalls without node disruption.
func (e *Engine) checkAutoscalerOptionsChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) {
	// The node autoscaler is detector-derived. When the component baseline could
	// not be read, skip its diff entirely (an unknown baseline is not "disabled")
	// rather than fabricating an in-place install change. The pod autoscaler is
	// config-only and unaffected.
	if e.componentBaselineUnknown(oldSpec) {
		e.checkAutoscalerPodScalarsChange(oldSpec.Autoscaler.Pod, newSpec.Autoscaler.Pod, result)

		return
	}

	oldNode := oldSpec.Autoscaler.Node
	newNode := newSpec.Autoscaler.Node

	e.checkAutoscalerNodeScalarsChange(oldNode, newNode, result)
	e.checkAutoscalerPodScalarsChange(oldSpec.Autoscaler.Pod, newSpec.Autoscaler.Pod, result)
	e.checkAutoscalerPoolChanges(oldNode.Pools, newNode.Pools, result)
}

// checkAutoscalerNodeScalarsChange emits in-place changes for scalar node-autoscaler fields.
func (e *Engine) checkAutoscalerNodeScalarsChange(
	oldNode, newNode v1alpha1.NodeAutoscalerConfig,
	result *clusterupdate.UpdateResult,
) {
	appendChange(result, "cluster.autoscaler.node.enabled",
		string(oldNode.Enabled), string(newNode.Enabled),
		string(v1alpha1.NodeAutoscalerEnabledDisabled),
		"enabling/disabling the node autoscaler triggers Helm install/uninstall",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.maxNodesTotal",
		strconv.Itoa(int(oldNode.MaxNodesTotal)), strconv.Itoa(int(newNode.MaxNodesTotal)), "0",
		"maxNodesTotal can be updated via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.expander",
		oldNode.Expander.String(), newNode.Expander.String(),
		string(v1alpha1.AutoscalerExpanderLeastWaste),
		"expander strategy can be updated via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.scaleDownUnneededTime",
		oldNode.ScaleDownUnneededTime, newNode.ScaleDownUnneededTime, "",
		"scaleDownUnneededTime can be updated via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.scaleDownUtilizationThreshold",
		oldNode.ScaleDownUtilizationThreshold, newNode.ScaleDownUtilizationThreshold, "",
		"scaleDownUtilizationThreshold can be updated via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.capacityBuffers",
		strconv.FormatBool(oldNode.CapacityBuffers), strconv.FormatBool(newNode.CapacityBuffers),
		"false",
		"capacity buffers can be toggled via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.ignoreDaemonsetsUtilization",
		strconv.FormatBool(oldNode.IgnoreDaemonsetsUtilization),
		strconv.FormatBool(newNode.IgnoreDaemonsetsUtilization),
		"false",
		"ignoreDaemonsetsUtilization can be toggled via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	// skipNodesWith* are *bool with an upstream default of true; a nil pointer
	// (unset) compares as "true" so nil→false surfaces as a change and nil↔true
	// does not.
	appendChange(result, "cluster.autoscaler.node.skipNodesWithLocalStorage",
		formatBoolPtr(oldNode.SkipNodesWithLocalStorage),
		formatBoolPtr(newNode.SkipNodesWithLocalStorage),
		"true",
		"skipNodesWithLocalStorage can be toggled via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.skipNodesWithSystemPods",
		formatBoolPtr(oldNode.SkipNodesWithSystemPods),
		formatBoolPtr(newNode.SkipNodesWithSystemPods),
		"true",
		"skipNodesWithSystemPods can be toggled via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)
}

// formatBoolPtr renders a *bool for diffing: a nil pointer yields the empty
// string so appendChange's default substitution supplies the field's upstream
// default, while a non-nil pointer renders its explicit true/false.
func formatBoolPtr(b *bool) string {
	if b == nil {
		return ""
	}

	return strconv.FormatBool(*b)
}

// checkAutoscalerPodScalarsChange emits in-place changes for scalar pod-autoscaler fields.
func (e *Engine) checkAutoscalerPodScalarsChange(
	oldPod, newPod v1alpha1.PodAutoscalerConfig,
	result *clusterupdate.UpdateResult,
) {
	appendChange(result, "cluster.autoscaler.pod.horizontal",
		string(oldPod.Horizontal), string(newPod.Horizontal),
		string(v1alpha1.PodAutoscalerHorizontalDisabled),
		"enabling/disabling HPA affects metrics-server dependency; applied in-place",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.pod.vertical",
		string(oldPod.Vertical), string(newPod.Vertical),
		string(v1alpha1.PodAutoscalerVerticalDisabled),
		"VPA setting recorded; installer wiring is reserved for a future release",
		clusterupdate.ChangeCategoryInPlace)
}

// poolsByName indexes a slice of NodePool by name, keeping only the first
// occurrence of each name (duplicates are silently discarded).
func poolsByName(pools []v1alpha1.NodePool) map[string]v1alpha1.NodePool {
	byName := make(map[string]v1alpha1.NodePool, len(pools))

	for _, p := range pools {
		if _, exists := byName[p.Name]; !exists {
			byName[p.Name] = p
		}
	}

	return byName
}

// checkAutoscalerPoolChanges compares old and new node pool slices by name and emits
// in-place diff entries for each addition, removal, or field-level modification.
func (e *Engine) checkAutoscalerPoolChanges(
	oldPools, newPools []v1alpha1.NodePool,
	result *clusterupdate.UpdateResult,
) {
	oldByName := poolsByName(oldPools)
	newByName := poolsByName(newPools)

	e.checkAutoscalerPoolsAdded(oldByName, newByName, result)
	e.checkAutoscalerPoolsRemoved(oldByName, newByName, result)
	e.checkAutoscalerPoolsModified(oldByName, newByName, result)
}

// checkAutoscalerPoolsAdded emits in-place changes for pools present in new but absent in old.
func (e *Engine) checkAutoscalerPoolsAdded(
	oldByName, newByName map[string]v1alpha1.NodePool,
	result *clusterupdate.UpdateResult,
) {
	addedNames := make([]string, 0, len(newByName))

	for name := range newByName {
		if _, exists := oldByName[name]; !exists {
			addedNames = append(addedNames, name)
		}
	}

	slices.Sort(addedNames)

	for _, name := range addedNames {
		appendChange(result, "cluster.autoscaler.node.pools["+name+"]",
			"", "Added",
			"", "node pool added; will be applied in-place via Helm chart upgrade",
			clusterupdate.ChangeCategoryInPlace)
	}
}

// checkAutoscalerPoolsRemoved emits in-place changes for pools present in old but absent in new.
func (e *Engine) checkAutoscalerPoolsRemoved(
	oldByName, newByName map[string]v1alpha1.NodePool,
	result *clusterupdate.UpdateResult,
) {
	removedNames := make([]string, 0, len(oldByName))

	for name := range oldByName {
		if _, exists := newByName[name]; !exists {
			removedNames = append(removedNames, name)
		}
	}

	slices.Sort(removedNames)

	for _, name := range removedNames {
		appendChange(result, "cluster.autoscaler.node.pools["+name+"]",
			"Removed", "",
			"", "node pool removed; will be applied in-place via Helm chart upgrade",
			clusterupdate.ChangeCategoryInPlace)
	}
}

// checkAutoscalerPoolsModified emits field-level in-place changes for pools present in both old and new.
func (e *Engine) checkAutoscalerPoolsModified(
	oldByName, newByName map[string]v1alpha1.NodePool,
	result *clusterupdate.UpdateResult,
) {
	modifiedNames := make([]string, 0, len(newByName))

	for name := range newByName {
		if _, exists := oldByName[name]; exists {
			modifiedNames = append(modifiedNames, name)
		}
	}

	slices.Sort(modifiedNames)

	for _, name := range modifiedNames {
		newPool := newByName[name]
		oldPool := oldByName[name]

		poolField := fmt.Sprintf("cluster.autoscaler.node.pools[%s]", name)

		appendChange(result, poolField+".serverType",
			oldPool.ServerType, newPool.ServerType, "",
			"pool serverType can be updated in-place via Helm chart upgrade",
			clusterupdate.ChangeCategoryInPlace)

		appendChange(result, poolField+".location",
			oldPool.Location, newPool.Location, "",
			"pool location can be updated in-place via Helm chart upgrade",
			clusterupdate.ChangeCategoryInPlace)

		appendChange(result, poolField+".min",
			strconv.Itoa(int(oldPool.Min)), strconv.Itoa(int(newPool.Min)), "",
			"pool min can be updated in-place via Helm chart upgrade",
			clusterupdate.ChangeCategoryInPlace)

		appendChange(result, poolField+".max",
			strconv.Itoa(int(oldPool.Max)), strconv.Itoa(int(newPool.Max)), "",
			"pool max can be updated in-place via Helm chart upgrade",
			clusterupdate.ChangeCategoryInPlace)

		appendChange(result, poolField+".labels",
			formatPoolLabels(oldPool.Labels), formatPoolLabels(newPool.Labels), "",
			"pool labels updated in-place (autoscaler config secret + Helm upgrade); "+
				"existing autoscaler nodes are recycled to pick up the change",
			clusterupdate.ChangeCategoryInPlace)

		appendChange(result, poolField+".taints",
			formatPoolTaints(oldPool.Taints), formatPoolTaints(newPool.Taints), "",
			"pool taints updated in-place (autoscaler config secret + Helm upgrade); "+
				"existing autoscaler nodes are recycled to pick up the change",
			clusterupdate.ChangeCategoryInPlace)
	}
}

// formatPoolLabels renders a pool's labels as a stable, sorted "k=v,k=v" string
// for diffing. The empty string represents no labels.
func formatPoolLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}

	return strings.Join(parts, ",")
}

// formatPoolTaints renders a pool's taints as a stable, sorted "k=v:Effect,..."
// string for diffing. Taints form a set on the node, so the output is sorted to
// avoid spurious diffs on reorder. The empty string represents no taints.
func formatPoolTaints(taints []v1alpha1.NodePoolTaint) string {
	if len(taints) == 0 {
		return ""
	}

	parts := make([]string, 0, len(taints))
	for _, taint := range taints {
		parts = append(parts, taint.Key+"="+taint.Value+":"+string(taint.Effect))
	}

	slices.Sort(parts)

	return strings.Join(parts, ",")
}

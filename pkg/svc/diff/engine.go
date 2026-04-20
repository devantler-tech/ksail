package diff

import (
	"strconv"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// Engine computes configuration differences and classifies their impact.
type Engine struct {
	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
}

// NewEngine creates a new diff engine for the given distribution and provider.
func NewEngine(distribution v1alpha1.Distribution, provider v1alpha1.Provider) *Engine {
	return &Engine{
		distribution: distribution,
		provider:     provider,
	}
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

	// Check simple scalar fields via table-driven rules
	e.applyFieldRules(oldSpec, newSpec, result, e.scalarFieldRules())

	// Check complex / distribution-specific changes
	e.checkLocalRegistryChange(oldSpec, newSpec, result)
	e.checkVanillaOptionsChange(oldSpec, newSpec, result)
	e.checkTalosOptionsChange(oldSpec, newSpec, result)
	e.checkHetznerOptionsChange(oldProvider, newProvider, result)

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
	if gitOpsEngine == v1alpha1.GitOpsEngineNone || gitOpsEngine == "" {
		return
	}

	appendChange(result, "cluster.workload.tag",
		oldTag, newTag, "",
		"workload tag can be updated in-place on the GitOps sync resource",
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
			field:    "cluster.cni",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "CNI can be switched via Helm upgrade/uninstall",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.CNI.String() },
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
				// K3s, VCluster, and KWOK have no CDI runtime wiring — suppress diffs.
				switch e.distribution {
				case v1alpha1.DistributionK3s,
					v1alpha1.DistributionVCluster,
					v1alpha1.DistributionKWOK,
					v1alpha1.DistributionEKS:
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
				return string(s.MetricsServer.EffectiveValue(e.distribution))
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
			field:    "cluster.certManager",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "cert-manager can be installed/uninstalled via Helm",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.CertManager.String() },
		},
		{
			field:    "cluster.policyEngine",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "policy engine can be switched via Helm install/uninstall",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.PolicyEngine.String() },
		},
		{
			field:    "cluster.gitOpsEngine",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "GitOps engine can be switched via Helm install/uninstall",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.GitOpsEngine.String() },
		},
	}
}

// appendChange appends a single diff change to the appropriate category slice in result.
// Both applyFieldRules and applyProviderFieldRules delegate to this helper to
// avoid duplicating the default-value substitution and category dispatch logic.
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

	change := clusterupdate.Change{
		Field:    field,
		OldValue: oldVal,
		NewValue: newVal,
		Category: category,
		Reason:   reason,
	}

	switch category {
	case clusterupdate.ChangeCategoryRecreateRequired:
		result.RecreateRequired = append(result.RecreateRequired, change)
	case clusterupdate.ChangeCategoryInPlace:
		result.InPlaceChanges = append(result.InPlaceChanges, change)
	case clusterupdate.ChangeCategoryRebootRequired:
		result.RebootRequired = append(result.RebootRequired, change)
	}
}

// applyFieldRules evaluates each field rule and appends changes to the result.
func (e *Engine) applyFieldRules(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
	rules []fieldRule,
) {
	for _, rule := range rules {
		cat := rule.category
		if rule.categoryFn != nil {
			cat = rule.categoryFn()
		}

		appendChange(result, rule.field,
			rule.getVal(oldSpec), rule.getVal(newSpec),
			rule.defaultVal, rule.reason, cat)
	}
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

	if oldSpec.LocalRegistry.Registry != newSpec.LocalRegistry.Registry {
		// For Kind, registry changes require recreate (containerd config is baked in)
		// For Talos/K3d, registry mirrors can be updated in-place
		switch e.distribution {
		case v1alpha1.DistributionVanilla:
			result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
				Field:    "cluster.localRegistry.registry",
				OldValue: oldSpec.LocalRegistry.Registry,
				NewValue: newSpec.LocalRegistry.Registry,
				Category: clusterupdate.ChangeCategoryRecreateRequired,
				Reason:   "Kind requires cluster recreate to change containerd registry config",
			})
		case v1alpha1.DistributionTalos, v1alpha1.DistributionK3s:
			reasons := map[v1alpha1.Distribution]string{
				v1alpha1.DistributionTalos: "Talos supports .machine.registries updates without reboot",
				v1alpha1.DistributionK3s:   "K3d supports registries.yaml updates",
			}
			result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
				Field:    "cluster.localRegistry.registry",
				OldValue: oldSpec.LocalRegistry.Registry,
				NewValue: newSpec.LocalRegistry.Registry,
				Category: clusterupdate.ChangeCategoryInPlace,
				Reason:   reasons[e.distribution],
			})
		case v1alpha1.DistributionVCluster, v1alpha1.DistributionKWOK, v1alpha1.DistributionEKS:
			result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
				Field:    "cluster.localRegistry.registry",
				OldValue: oldSpec.LocalRegistry.Registry,
				NewValue: newSpec.LocalRegistry.Registry,
				Category: clusterupdate.ChangeCategoryInPlace,
				Reason:   "VCluster/KWOK/EKS manage registry independently of the node OS",
			})
		}
	}
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
	if oldSpec.Vanilla.MirrorsDir != newSpec.Vanilla.MirrorsDir {
		result.RecreateRequired = append(result.RecreateRequired, clusterupdate.Change{
			Field:    "cluster.vanilla.mirrorsDir",
			OldValue: oldSpec.Vanilla.MirrorsDir,
			NewValue: newSpec.Vanilla.MirrorsDir,
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "Kind containerd mirror config is baked at cluster creation",
		})
	}
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
		field:    "cluster.talos.controlPlanes",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "Talos supports adding/removing control-plane nodes via provider",
		getVal:   func(s *v1alpha1.ClusterSpec) string { return strconv.Itoa(int(s.Talos.ControlPlanes)) },
	},
	{
		field:    "cluster.talos.workers",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "Talos supports adding/removing worker nodes via provider",
		getVal:   func(s *v1alpha1.ClusterSpec) string { return strconv.Itoa(int(s.Talos.Workers)) },
	},
	{
		field:    "cluster.talos.iso",
		category: clusterupdate.ChangeCategoryInPlace,
		reason:   "ISO change only affects newly provisioned nodes",
		getVal:   func(s *v1alpha1.ClusterSpec) string { return strconv.FormatInt(s.Talos.ISO, 10) },
	},
}

// checkHetznerOptionsChange checks Hetzner-specific option changes.
func (e *Engine) checkHetznerOptionsChange(
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
		field:      "provider.hetzner.controlPlaneServerType",
		category:   clusterupdate.ChangeCategoryRecreateRequired,
		reason:     "existing control-plane servers cannot change VM type",
		getVal:     func(s *v1alpha1.ProviderSpec) string { return s.Hetzner.ControlPlaneServerType },
		defaultVal: v1alpha1.DefaultHetznerServerType,
	},
	{
		field:      "provider.hetzner.workerServerType",
		category:   clusterupdate.ChangeCategoryInPlace,
		reason:     "new worker servers will use the new type; existing workers unchanged",
		getVal:     func(s *v1alpha1.ProviderSpec) string { return s.Hetzner.WorkerServerType },
		defaultVal: v1alpha1.DefaultHetznerServerType,
	},
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

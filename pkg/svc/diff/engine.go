package diff

import (
	"fmt"
	"sort"
	"strconv"

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

	// Check simple scalar fields via table-driven rules.
	// Lazily initialize rules in case Engine was constructed without NewEngine.
	if e.rules == nil {
		e.rules = e.scalarFieldRules()
	}
	e.applyFieldRules(oldSpec, newSpec, result, e.rules)

	// Check complex / distribution-specific changes
	e.checkLocalRegistryChange(oldSpec, newSpec, result)
	e.checkVanillaOptionsChange(oldSpec, newSpec, result)
	e.checkTalosOptionsChange(oldSpec, newSpec, result)
	e.checkHetznerOptionsChange(oldProvider, newProvider, result)
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
		reason:   "VCluster/KWOK/EKS manage registry independently of the node OS",
		category: clusterupdate.ChangeCategoryInPlace,
	},
	v1alpha1.DistributionKWOK: {
		reason:   "VCluster/KWOK/EKS manage registry independently of the node OS",
		category: clusterupdate.ChangeCategoryInPlace,
	},
	v1alpha1.DistributionEKS: {
		reason:   "VCluster/KWOK/EKS manage registry independently of the node OS",
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

	if rc, ok := localRegistryReasonMap[e.distribution]; ok {
		appendChange(result, "cluster.localRegistry.registry",
			oldSpec.LocalRegistry.Registry, newSpec.LocalRegistry.Registry,
			"", rc.reason, rc.category)
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
//
//nolint:gochecknoglobals // Immutable field-rule; avoids per-call heap allocation.
var talosISORule = fieldRule{
	field:    "cluster.talos.iso",
	category: clusterupdate.ChangeCategoryInPlace,
	reason:   "ISO change only affects newly provisioned nodes",
	getVal:   func(s *v1alpha1.ClusterSpec) string { return strconv.FormatInt(s.Talos.ISO, 10) },
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
		string(oldNode.Expander), string(newNode.Expander),
		string(v1alpha1.AutoscalerExpanderLeastWaste),
		"expander strategy can be updated via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)

	appendChange(result, "cluster.autoscaler.node.scaleDownUnneededTime",
		oldNode.ScaleDownUnneededTime, newNode.ScaleDownUnneededTime, "",
		"scaleDownUnneededTime can be updated via Helm chart upgrade",
		clusterupdate.ChangeCategoryInPlace)
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

// checkAutoscalerPoolChanges compares old and new node pool slices by name and emits
// in-place diff entries for each addition, removal, or field-level modification.
func (e *Engine) checkAutoscalerPoolChanges(
	oldPools, newPools []v1alpha1.NodePool,
	result *clusterupdate.UpdateResult,
) {
	oldByName := make(map[string]v1alpha1.NodePool, len(oldPools))

	for _, p := range oldPools {
		if _, exists := oldByName[p.Name]; !exists {
			oldByName[p.Name] = p
		}
	}

	newByName := make(map[string]v1alpha1.NodePool, len(newPools))

	for _, p := range newPools {
		if _, exists := newByName[p.Name]; !exists {
			newByName[p.Name] = p
		}
	}

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

	sort.Strings(addedNames)

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

	sort.Strings(removedNames)

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

	sort.Strings(modifiedNames)

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
	}
}

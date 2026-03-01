package diff

import (
	"strconv"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clusterupdate"
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

// ComputeDiff compares old and new ClusterSpec and categorizes all changes.
func (e *Engine) ComputeDiff(oldSpec, newSpec *v1alpha1.ClusterSpec) *clusterupdate.UpdateResult {
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
	e.checkHetznerOptionsChange(oldSpec, newSpec, result)

	return result
}

// fieldRule describes how to diff a single scalar field.
type fieldRule struct {
	field    string
	category clusterupdate.ChangeCategory
	reason   string
	// getVal extracts the string representation of this field from a ClusterSpec.
	getVal func(*v1alpha1.ClusterSpec) string
}

// scalarFieldRules returns the table of simple scalar field diff rules.
// These fields follow a uniform pattern: compare old vs new, emit a Change if different.
// For CSI, MetricsServer, and LoadBalancer the effective value is used so that
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
			getVal: func(s *v1alpha1.ClusterSpec) string {
				return string(s.CSI.EffectiveValue(e.distribution, e.provider))
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

// applyFieldRules evaluates each field rule and appends changes to the result.
func (e *Engine) applyFieldRules(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
	rules []fieldRule,
) {
	for _, rule := range rules {
		oldVal := rule.getVal(oldSpec)
		newVal := rule.getVal(newSpec)

		if oldVal == newVal {
			continue
		}

		change := clusterupdate.Change{
			Field:    rule.field,
			OldValue: oldVal,
			NewValue: newVal,
			Category: rule.category,
			Reason:   rule.reason,
		}

		switch rule.category {
		case clusterupdate.ChangeCategoryRecreateRequired:
			result.RecreateRequired = append(result.RecreateRequired, change)
		case clusterupdate.ChangeCategoryInPlace:
			result.InPlaceChanges = append(result.InPlaceChanges, change)
		case clusterupdate.ChangeCategoryRebootRequired:
			result.RebootRequired = append(result.RebootRequired, change)
		}
	}
}

// checkLocalRegistryChange checks if local registry config has changed.
func (e *Engine) checkLocalRegistryChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) {
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
		case v1alpha1.DistributionVCluster:
			result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
				Field:    "cluster.localRegistry.registry",
				OldValue: oldSpec.LocalRegistry.Registry,
				NewValue: newSpec.LocalRegistry.Registry,
				Category: clusterupdate.ChangeCategoryInPlace,
				Reason:   "VCluster manages registry independently via Docker networking",
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

	e.applyFieldRules(oldSpec, newSpec, result, talosFieldRules())
}

// talosFieldRules returns the Talos-specific scalar field diff rules.
func talosFieldRules() []fieldRule {
	return []fieldRule{
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
}

// checkHetznerOptionsChange checks Hetzner-specific option changes.
func (e *Engine) checkHetznerOptionsChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *clusterupdate.UpdateResult,
) {
	if e.provider != v1alpha1.ProviderHetzner {
		return
	}

	e.applyFieldRules(oldSpec, newSpec, result, hetznerFieldRules())
}

// hetznerFieldRules returns the Hetzner-specific scalar field diff rules.
func hetznerFieldRules() []fieldRule {
	return []fieldRule{
		{
			field:    "cluster.hetzner.controlPlaneServerType",
			category: clusterupdate.ChangeCategoryRecreateRequired,
			reason:   "existing control-plane servers cannot change VM type",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Hetzner.ControlPlaneServerType },
		},
		{
			field:    "cluster.hetzner.workerServerType",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "new worker servers will use the new type; existing workers unchanged",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Hetzner.WorkerServerType },
		},
		{
			field:    "cluster.hetzner.location",
			category: clusterupdate.ChangeCategoryRecreateRequired,
			reason:   "datacenter location cannot be changed for existing servers",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Hetzner.Location },
		},
		{
			field:    "cluster.hetzner.networkName",
			category: clusterupdate.ChangeCategoryRecreateRequired,
			reason:   "cannot migrate servers between networks",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Hetzner.NetworkName },
		},
		{
			field:    "cluster.hetzner.networkCidr",
			category: clusterupdate.ChangeCategoryRecreateRequired,
			reason:   "network CIDR change requires PKI regeneration",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Hetzner.NetworkCIDR },
		},
		{
			field:    "cluster.hetzner.sshKeyName",
			category: clusterupdate.ChangeCategoryInPlace,
			reason:   "SSH key change only affects newly provisioned servers",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Hetzner.SSHKeyName },
		},
	}
}

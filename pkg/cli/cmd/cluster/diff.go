package cluster

import (
	"strconv"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
)

// DiffEngine computes configuration differences and classifies their impact.
type DiffEngine struct {
	distribution v1alpha1.Distribution
	provider     v1alpha1.Provider
}

// NewDiffEngine creates a new diff engine for the given distribution and provider.
func NewDiffEngine(distribution v1alpha1.Distribution, provider v1alpha1.Provider) *DiffEngine {
	return &DiffEngine{
		distribution: distribution,
		provider:     provider,
	}
}

// ComputeDiff compares old and new ClusterSpec and categorizes all changes.
func (e *DiffEngine) ComputeDiff(oldSpec, newSpec *v1alpha1.ClusterSpec) *types.UpdateResult {
	result := types.NewEmptyUpdateResult()

	if oldSpec == nil || newSpec == nil {
		return result
	}

	// Check simple scalar fields via table-driven rules
	e.applyFieldRules(oldSpec, newSpec, result)

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
	category types.ChangeCategory
	reason   string
	// getVal extracts the string representation of this field from a ClusterSpec.
	getVal func(*v1alpha1.ClusterSpec) string
}

// scalarFieldRules returns the table of simple scalar field diff rules.
// These fields follow a uniform pattern: compare old vs new, emit a Change if different.
func scalarFieldRules() []fieldRule {
	return []fieldRule{
		{
			field:    "cluster.distribution",
			category: types.ChangeCategoryRecreateRequired,
			reason:   "changing the Kubernetes distribution requires cluster recreation",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Distribution.String() },
		},
		{
			field:    "cluster.provider",
			category: types.ChangeCategoryRecreateRequired,
			reason:   "changing the infrastructure provider requires cluster recreation",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.Provider.String() },
		},
		{
			field:    "cluster.cni",
			category: types.ChangeCategoryInPlace,
			reason:   "CNI can be switched via Helm upgrade/uninstall",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.CNI.String() },
		},
		{
			field:    "cluster.csi",
			category: types.ChangeCategoryInPlace,
			reason:   "CSI can be switched via Helm install/uninstall",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.CSI.String() },
		},
		{
			field:    "cluster.metricsServer",
			category: types.ChangeCategoryInPlace,
			reason:   "metrics-server can be installed/uninstalled via Helm",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.MetricsServer.String() },
		},
		{
			field:    "cluster.loadBalancer",
			category: types.ChangeCategoryInPlace,
			reason:   "load balancer can be enabled/disabled via Helm",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.LoadBalancer.String() },
		},
		{
			field:    "cluster.certManager",
			category: types.ChangeCategoryInPlace,
			reason:   "cert-manager can be installed/uninstalled via Helm",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.CertManager.String() },
		},
		{
			field:    "cluster.policyEngine",
			category: types.ChangeCategoryInPlace,
			reason:   "policy engine can be switched via Helm install/uninstall",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.PolicyEngine.String() },
		},
		{
			field:    "cluster.gitOpsEngine",
			category: types.ChangeCategoryInPlace,
			reason:   "GitOps engine can be switched via Helm install/uninstall",
			getVal:   func(s *v1alpha1.ClusterSpec) string { return s.GitOpsEngine.String() },
		},
	}
}

// applyFieldRules evaluates each scalar field rule and appends changes to the result.
func (e *DiffEngine) applyFieldRules(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	for _, rule := range scalarFieldRules() {
		oldVal := rule.getVal(oldSpec)
		newVal := rule.getVal(newSpec)

		if oldVal == newVal {
			continue
		}

		change := types.Change{
			Field:    rule.field,
			OldValue: oldVal,
			NewValue: newVal,
			Category: rule.category,
			Reason:   rule.reason,
		}

		switch rule.category {
		case types.ChangeCategoryRecreateRequired:
			result.RecreateRequired = append(result.RecreateRequired, change)
		case types.ChangeCategoryInPlace:
			result.InPlaceChanges = append(result.InPlaceChanges, change)
		case types.ChangeCategoryRebootRequired:
			result.RebootRequired = append(result.RebootRequired, change)
		}
	}
}

// checkLocalRegistryChange checks if local registry config has changed.
func (e *DiffEngine) checkLocalRegistryChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.LocalRegistry.Registry != newSpec.LocalRegistry.Registry {
		// For Kind, registry changes require recreate (containerd config is baked in)
		// For Talos/K3d, registry mirrors can be updated in-place
		switch e.distribution {
		case v1alpha1.DistributionVanilla:
			result.RecreateRequired = append(result.RecreateRequired, types.Change{
				Field:    "cluster.localRegistry.registry",
				OldValue: oldSpec.LocalRegistry.Registry,
				NewValue: newSpec.LocalRegistry.Registry,
				Category: types.ChangeCategoryRecreateRequired,
				Reason:   "Kind requires cluster recreate to change containerd registry config",
			})
		case v1alpha1.DistributionTalos:
			result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
				Field:    "cluster.localRegistry.registry",
				OldValue: oldSpec.LocalRegistry.Registry,
				NewValue: newSpec.LocalRegistry.Registry,
				Category: types.ChangeCategoryInPlace,
				Reason:   "Talos supports .machine.registries updates without reboot",
			})
		case v1alpha1.DistributionK3s:
			result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
				Field:    "cluster.localRegistry.registry",
				OldValue: oldSpec.LocalRegistry.Registry,
				NewValue: newSpec.LocalRegistry.Registry,
				Category: types.ChangeCategoryInPlace,
				Reason:   "K3d supports registries.yaml updates",
			})
		}
	}
}

// checkVanillaOptionsChange checks Vanilla (Kind) specific option changes.
func (e *DiffEngine) checkVanillaOptionsChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if e.distribution != v1alpha1.DistributionVanilla {
		return
	}

	// MirrorsDir change requires recreate (containerd config is baked at creation)
	if oldSpec.Vanilla.MirrorsDir != newSpec.Vanilla.MirrorsDir {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "cluster.vanilla.mirrorsDir",
			OldValue: oldSpec.Vanilla.MirrorsDir,
			NewValue: newSpec.Vanilla.MirrorsDir,
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "Kind containerd mirror config is baked at cluster creation",
		})
	}
}

// checkTalosOptionsChange checks Talos-specific option changes.
func (e *DiffEngine) checkTalosOptionsChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if e.distribution != v1alpha1.DistributionTalos {
		return
	}

	// Control plane count change - can scale via provider
	if oldSpec.Talos.ControlPlanes != newSpec.Talos.ControlPlanes {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.talos.controlPlanes",
			OldValue: strconv.Itoa(int(oldSpec.Talos.ControlPlanes)),
			NewValue: strconv.Itoa(int(newSpec.Talos.ControlPlanes)),
			Category: types.ChangeCategoryInPlace,
			Reason:   "Talos supports adding/removing control-plane nodes via provider",
		})
	}

	// Worker count change - can scale via provider
	if oldSpec.Talos.Workers != newSpec.Talos.Workers {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.talos.workers",
			OldValue: strconv.Itoa(int(oldSpec.Talos.Workers)),
			NewValue: strconv.Itoa(int(newSpec.Talos.Workers)),
			Category: types.ChangeCategoryInPlace,
			Reason:   "Talos supports adding/removing worker nodes via provider",
		})
	}

	// ISO change doesn't affect existing nodes, only new ones
	if oldSpec.Talos.ISO != newSpec.Talos.ISO {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.talos.iso",
			OldValue: strconv.FormatInt(oldSpec.Talos.ISO, 10),
			NewValue: strconv.FormatInt(newSpec.Talos.ISO, 10),
			Category: types.ChangeCategoryInPlace,
			Reason:   "ISO change only affects newly provisioned nodes",
		})
	}
}

// checkHetznerOptionsChange checks Hetzner-specific option changes.
//
//nolint:funlen // Multiple Hetzner options need to be checked sequentially
func (e *DiffEngine) checkHetznerOptionsChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if e.provider != v1alpha1.ProviderHetzner {
		return
	}

	// Server type changes require node replacement
	if oldSpec.Hetzner.ControlPlaneServerType != newSpec.Hetzner.ControlPlaneServerType {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "cluster.hetzner.controlPlaneServerType",
			OldValue: oldSpec.Hetzner.ControlPlaneServerType,
			NewValue: newSpec.Hetzner.ControlPlaneServerType,
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "existing control-plane servers cannot change VM type",
		})
	}

	// Worker server type - new workers use new type, existing keep old
	if oldSpec.Hetzner.WorkerServerType != newSpec.Hetzner.WorkerServerType {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.hetzner.workerServerType",
			OldValue: oldSpec.Hetzner.WorkerServerType,
			NewValue: newSpec.Hetzner.WorkerServerType,
			Category: types.ChangeCategoryInPlace,
			Reason:   "new worker servers will use the new type; existing workers unchanged",
		})
	}

	// Location change requires full recreate
	if oldSpec.Hetzner.Location != newSpec.Hetzner.Location {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "cluster.hetzner.location",
			OldValue: oldSpec.Hetzner.Location,
			NewValue: newSpec.Hetzner.Location,
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "datacenter location cannot be changed for existing servers",
		})
	}

	// Network name change requires recreate
	if oldSpec.Hetzner.NetworkName != newSpec.Hetzner.NetworkName {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "cluster.hetzner.networkName",
			OldValue: oldSpec.Hetzner.NetworkName,
			NewValue: newSpec.Hetzner.NetworkName,
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "cannot migrate servers between networks",
		})
	}

	// Network CIDR change requires recreate
	if oldSpec.Hetzner.NetworkCIDR != newSpec.Hetzner.NetworkCIDR {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "cluster.hetzner.networkCidr",
			OldValue: oldSpec.Hetzner.NetworkCIDR,
			NewValue: newSpec.Hetzner.NetworkCIDR,
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "network CIDR change requires PKI regeneration",
		})
	}

	// SSH key change only affects new servers
	if oldSpec.Hetzner.SSHKeyName != newSpec.Hetzner.SSHKeyName {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.hetzner.sshKeyName",
			OldValue: oldSpec.Hetzner.SSHKeyName,
			NewValue: newSpec.Hetzner.SSHKeyName,
			Category: types.ChangeCategoryInPlace,
			Reason:   "SSH key change only affects newly provisioned servers",
		})
	}
}

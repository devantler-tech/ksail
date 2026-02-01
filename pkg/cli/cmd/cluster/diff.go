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
	result := &types.UpdateResult{
		InPlaceChanges:   make([]types.Change, 0),
		RebootRequired:   make([]types.Change, 0),
		RecreateRequired: make([]types.Change, 0),
	}

	if oldSpec == nil || newSpec == nil {
		return result
	}

	// Check distribution change (always requires recreate)
	e.checkDistributionChange(oldSpec, newSpec, result)

	// Check provider change (always requires recreate)
	e.checkProviderChange(oldSpec, newSpec, result)

	// Check component changes (usually in-place via Helm)
	e.checkCNIChange(oldSpec, newSpec, result)
	e.checkCSIChange(oldSpec, newSpec, result)
	e.checkMetricsServerChange(oldSpec, newSpec, result)
	e.checkCertManagerChange(oldSpec, newSpec, result)
	e.checkPolicyEngineChange(oldSpec, newSpec, result)
	e.checkGitOpsEngineChange(oldSpec, newSpec, result)
	e.checkLocalRegistryChange(oldSpec, newSpec, result)

	// Check distribution-specific options
	e.checkVanillaOptionsChange(oldSpec, newSpec, result)
	e.checkTalosOptionsChange(oldSpec, newSpec, result)
	e.checkHetznerOptionsChange(oldSpec, newSpec, result)

	return result
}

// checkDistributionChange checks if the distribution has changed.
func (e *DiffEngine) checkDistributionChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.Distribution != newSpec.Distribution {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "cluster.distribution",
			OldValue: oldSpec.Distribution.String(),
			NewValue: newSpec.Distribution.String(),
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "changing the Kubernetes distribution requires cluster recreation",
		})
	}
}

// checkProviderChange checks if the provider has changed.
func (e *DiffEngine) checkProviderChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.Provider != newSpec.Provider {
		result.RecreateRequired = append(result.RecreateRequired, types.Change{
			Field:    "cluster.provider",
			OldValue: oldSpec.Provider.String(),
			NewValue: newSpec.Provider.String(),
			Category: types.ChangeCategoryRecreateRequired,
			Reason:   "changing the infrastructure provider requires cluster recreation",
		})
	}
}

// checkCNIChange checks if the CNI has changed.
func (e *DiffEngine) checkCNIChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.CNI != newSpec.CNI {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.cni",
			OldValue: oldSpec.CNI.String(),
			NewValue: newSpec.CNI.String(),
			Category: types.ChangeCategoryInPlace,
			Reason:   "CNI can be switched via Helm upgrade/uninstall",
		})
	}
}

// checkCSIChange checks if the CSI has changed.
func (e *DiffEngine) checkCSIChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.CSI != newSpec.CSI {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.csi",
			OldValue: oldSpec.CSI.String(),
			NewValue: newSpec.CSI.String(),
			Category: types.ChangeCategoryInPlace,
			Reason:   "CSI can be switched via Helm install/uninstall",
		})
	}
}

// checkMetricsServerChange checks if the metrics server setting has changed.
func (e *DiffEngine) checkMetricsServerChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.MetricsServer != newSpec.MetricsServer {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.metricsServer",
			OldValue: oldSpec.MetricsServer.String(),
			NewValue: newSpec.MetricsServer.String(),
			Category: types.ChangeCategoryInPlace,
			Reason:   "metrics-server can be installed/uninstalled via Helm",
		})
	}
}

// checkCertManagerChange checks if cert-manager setting has changed.
func (e *DiffEngine) checkCertManagerChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.CertManager != newSpec.CertManager {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.certManager",
			OldValue: oldSpec.CertManager.String(),
			NewValue: newSpec.CertManager.String(),
			Category: types.ChangeCategoryInPlace,
			Reason:   "cert-manager can be installed/uninstalled via Helm",
		})
	}
}

// checkPolicyEngineChange checks if the policy engine has changed.
func (e *DiffEngine) checkPolicyEngineChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.PolicyEngine != newSpec.PolicyEngine {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.policyEngine",
			OldValue: oldSpec.PolicyEngine.String(),
			NewValue: newSpec.PolicyEngine.String(),
			Category: types.ChangeCategoryInPlace,
			Reason:   "policy engine can be switched via Helm install/uninstall",
		})
	}
}

// checkGitOpsEngineChange checks if the GitOps engine has changed.
func (e *DiffEngine) checkGitOpsEngineChange(
	oldSpec, newSpec *v1alpha1.ClusterSpec,
	result *types.UpdateResult,
) {
	if oldSpec.GitOpsEngine != newSpec.GitOpsEngine {
		result.InPlaceChanges = append(result.InPlaceChanges, types.Change{
			Field:    "cluster.gitOpsEngine",
			OldValue: oldSpec.GitOpsEngine.String(),
			NewValue: newSpec.GitOpsEngine.String(),
			Category: types.ChangeCategoryInPlace,
			Reason:   "GitOps engine can be switched via Helm install/uninstall",
		})
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

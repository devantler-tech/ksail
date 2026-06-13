package talosprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

// GetCurrentConfig retrieves the current cluster configuration by probing the
// running cluster through the Kubernetes API and Docker/Hetzner/Omni providers.
func (p *Provisioner) GetCurrentConfig(
	ctx context.Context,
	clusterName string,
) (*v1alpha1.ClusterSpec, *v1alpha1.ProviderSpec, error) {
	var provider v1alpha1.Provider

	switch {
	case p.dockerClient != nil:
		provider = v1alpha1.ProviderDocker
	case p.hetznerOpts != nil:
		provider = v1alpha1.ProviderHetzner
	case p.omniOpts != nil:
		provider = v1alpha1.ProviderOmni
	}

	spec := clusterupdate.DefaultCurrentSpec(v1alpha1.DistributionTalos, provider)

	// Merge non-introspectable fields from persisted state.
	// Fields like Talos.ISO and LocalRegistry cannot be detected from the running
	// cluster but are saved after successful create/update operations.
	err := p.mergePersistedState(spec, clusterName)
	if err != nil {
		return nil, nil, fmt.Errorf("merge persisted state: %w", err)
	}

	// Detect installed components from the live cluster when the detector is available.
	// A detection failure means the cluster is unreachable (kube API down, stale
	// kubeconfig). Propagate it instead of silently falling back to a default
	// baseline, which would make update propose a full reinstall of healthy
	// components and only fail later mid-apply.
	if p.componentDetector != nil {
		detected, err := p.componentDetector.DetectComponents(
			ctx,
			v1alpha1.DistributionTalos,
			provider,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("detect components: %w", err)
		}

		clusterupdate.ApplyDetectedComponents(spec, detected)
	}

	// Introspect actual node counts from the running cluster
	// to avoid false-positive diffs from hardcoded defaults.
	controlPlanes, workers := p.introspectNodeCounts(ctx)
	spec.ControlPlanes = controlPlanes
	spec.Workers = workers

	// Detect running Talos OS and Kubernetes versions to avoid false-positive
	// diffs when the user pins those versions in config.
	p.introspectVersions(ctx, spec)

	// Build provider spec if we have Hetzner options configured.
	// Server types are introspected from the running Hetzner servers so
	// that changes (e.g., cx22 -> cx33) appear in the diff. Other Hetzner
	// fields (location, network, SSH key) cannot be introspected, so we
	// echo the desired config as the baseline for those.
	var providerSpec *v1alpha1.ProviderSpec

	if p.hetznerOpts != nil {
		hetznerSpec := *p.hetznerOpts
		p.introspectHetznerServerTypes(ctx, &hetznerSpec)

		providerSpec = &v1alpha1.ProviderSpec{
			Hetzner: hetznerSpec,
		}
	}

	return spec, providerSpec, nil
}

// mergePersistedState resolves the cluster name and merges the previously saved
// ClusterSpec's non-introspectable fields (Talos ISO, local registry, mirrors
// directory) onto spec via the shared clusterupdate.MergePersistedState helper.
// This prevents false-positive diffs for boot-time settings and configuration
// that is not exposed via any cluster API.
func (p *Provisioner) mergePersistedState(spec *v1alpha1.ClusterSpec, clusterName string) error {
	name := p.resolveClusterName(clusterName)

	//nolint:wrapcheck // helper already wraps with cluster-name context
	return clusterupdate.MergePersistedState(spec, name)
}

// introspectNodeCounts determines the actual control-plane and worker node
// counts from the running cluster. Falls back to safe defaults (1 CP, 0 workers)
// when the cluster cannot be queried.
func (p *Provisioner) introspectNodeCounts(ctx context.Context) (int32, int32) {
	clusterName := p.resolveClusterName("")

	if p.dockerClient != nil {
		nodes, err := p.getDockerNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	if p.hetznerOpts != nil {
		nodes, err := p.getHetznerNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	if p.omniOpts != nil {
		nodes, err := p.getOmniNodesByRole(ctx, clusterName)
		if err == nil {
			return countNodeRoles(nodes)
		}
	}

	return 1, 0
}

// introspectVersions sets the running Talos OS and Kubernetes versions on spec so
// that a pinned spec.cluster.talos.version / spec.cluster.kubernetesVersion that
// matches the cluster does not read as a change. Either is left empty when the
// cluster cannot be reached.
func (p *Provisioner) introspectVersions(ctx context.Context, spec *v1alpha1.ClusterSpec) {
	spec.Talos.Version = p.introspectTalosVersion(ctx)
	spec.KubernetesVersion = p.introspectKubernetesVersion(ctx)
}

// introspectTalosVersion queries a control-plane node for the running Talos
// version. Returns an empty string when the version cannot be determined
// (e.g., no Talos API access); in that case the diff engine will report a
// version change if the desired spec specifies a version.
func (p *Provisioner) introspectTalosVersion(ctx context.Context) string {
	clusterName := p.resolveClusterName("")

	nodes, err := p.getNodesByRole(ctx, clusterName)
	if err != nil || len(nodes) == 0 {
		return ""
	}

	// Prefer a control-plane node for the version query; fall back to the
	// first available node if no control-plane node is found.
	target := nodes[0]

	for _, node := range nodes {
		if node.Role == RoleControlPlane {
			target = node

			break
		}
	}

	version, err := p.getRunningTalosVersion(ctx, target.IP)
	if err != nil {
		return ""
	}

	return version
}

// introspectKubernetesVersion reports the Kubernetes version running on the
// cluster, read from a control-plane node's machine config (kube-apiserver image
// tag). Returns an empty string when the version cannot be determined (e.g. no
// control-plane node reachable, no Talos API access, or an Omni-managed cluster);
// in that case the diff engine simply has no baseline to compare a pinned version
// against. Omni-managed clusters are skipped because Omni owns node configuration.
func (p *Provisioner) introspectKubernetesVersion(ctx context.Context) string {
	if p.omniOpts != nil {
		return ""
	}

	clusterName := p.resolveClusterName("")

	running, found, err := p.fetchRunningControlPlaneConfig(ctx, clusterName)
	if err != nil || !found {
		return ""
	}

	return talosconfigmanager.KubernetesVersionFromProvider(running)
}

// introspectHetznerServerTypes populates the ControlPlaneServerType and
// WorkerServerType fields on hetznerSpec from the running Hetzner servers.
func (p *Provisioner) introspectHetznerServerTypes(
	ctx context.Context,
	hetznerSpec *v1alpha1.OptionsHetzner,
) {
	if p.infraProvider == nil {
		return
	}

	clusterName := p.resolveClusterName("")

	nodes, listErr := p.infraProvider.ListNodes(ctx, clusterName)
	if listErr != nil {
		return
	}

	cpType := representativeServerType(nodes, RoleControlPlane, hetznerSpec.ControlPlaneServerType)
	if cpType != "" {
		hetznerSpec.ControlPlaneServerType = cpType
	}

	workerType := representativeServerType(nodes, RoleWorker, hetznerSpec.WorkerServerType)
	if workerType != "" {
		hetznerSpec.WorkerServerType = workerType
	}
}

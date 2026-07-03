package clusterdiscovery

import (
	"context"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
)

// dockerRunState reports a Docker-based cluster's run-state, dispatching to the injected DockerStatus
// seam when present (tests) and otherwise to a real docker-provider probe. Any error is swallowed to
// RunStateUnknown so a status probe never turns into a discovery failure or hides the cluster.
func (d *Discoverer) dockerRunState(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	name string,
) RunState {
	if d.DockerStatus != nil {
		return d.DockerStatus(ctx, distribution, name)
	}

	return defaultDockerRunState(ctx, distribution, name)
}

// defaultDockerRunState queries the Docker provider for a cluster's status and maps its provider-level
// phase to a RunState. It builds a fresh docker client per call (status is read on demand, not on a
// hot path) and returns RunStateUnknown on any failure so discovery stays best-effort.
func defaultDockerRunState(
	ctx context.Context,
	distribution v1alpha1.Distribution,
	name string,
) RunState {
	scheme, ok := dockerLabelScheme(distribution)
	if !ok {
		return RunStateUnknown
	}

	cli, err := dockerclient.GetDockerClient()
	if err != nil {
		return RunStateUnknown
	}

	defer func() { _ = cli.Close() }()

	status, err := dockerprovider.NewProvider(cli, scheme).GetClusterStatus(ctx, name)
	if err != nil || status == nil {
		return RunStateUnknown
	}

	if status.Ready {
		return RunStateRunning
	}

	return RunStateStopped
}

// dockerLabelScheme maps a Docker-based distribution to the container label scheme its nodes carry, so
// the docker provider can find them. It returns ok=false for distributions the docker provider cannot
// introspect for run-state (none today among LocalDistributions, but kept total for safety).
func dockerLabelScheme(
	distribution v1alpha1.Distribution,
) (dockerprovider.LabelScheme, bool) {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return dockerprovider.LabelSchemeKind, true
	case v1alpha1.DistributionK3s:
		return dockerprovider.LabelSchemeK3d, true
	case v1alpha1.DistributionTalos:
		return dockerprovider.LabelSchemeTalos, true
	case v1alpha1.DistributionVCluster:
		return dockerprovider.LabelSchemeVCluster, true
	case v1alpha1.DistributionKWOK:
		return dockerprovider.LabelSchemeKWOK, true
	case v1alpha1.DistributionEKS, v1alpha1.DistributionGKE:
		return "", false
	default:
		return "", false
	}
}

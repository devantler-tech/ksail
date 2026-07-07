// Package clusterdiscovery enumerates KSail-managed clusters across every infrastructure provider
// (Docker, Hetzner, Omni, AWS/EKS, GCP/GKE, Azure/AKS, and nested Kubernetes), reading provider
// credentials from the environment via a credentials.Resolver and silently skipping providers that
// are not configured.
//
// It is the single source of truth for "what clusters exist" shared by the `ksail cluster list`
// command and the local web-UI backend (pkg/cli/clusterapi). Before this package existed the two
// listers had drifted: the CLI queried all providers while the UI only saw local Docker clusters,
// so cloud clusters (e.g. a Talos production cluster on Hetzner/Omni) were invisible in the UI.
package clusterdiscovery

import (
	"context"
	"fmt"
	"sync"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// RunState is the coarse running/stopped state of a discovered cluster's infrastructure, distinct
// from its Kubernetes lifecycle phase. Docker-based clusters can be stopped (their containers exist
// but are not running) yet still appear in discovery, so this lets the local web-UI backend render a
// Docker-stopped cluster as not-Ready instead of falsely green. It is best-effort: providers that do
// not report it (cloud providers today) leave it RunStateUnknown.
type RunState string

const (
	// RunStateUnknown means run-state was not determined (cloud providers, or a status probe that
	// failed). Callers treat it as "no run-state signal" rather than stopped.
	RunStateUnknown RunState = ""
	// RunStateRunning means every node/container of the cluster is running.
	RunStateRunning RunState = "running"
	// RunStateStopped means the cluster's nodes/containers exist but none (or not all) are running —
	// the cluster is present but not serving. The local backend surfaces this as a not-Ready condition.
	RunStateStopped RunState = "stopped"
	// RunStateUnmanaged marks a cluster ksail did not provision — a kubeconfig context with no
	// matching managed cluster (see DiscoverUnmanaged). It carries no Distribution/Provider and is
	// never actionable by ksail-only operations. Distinct from RunStateUnknown, which is a managed
	// cluster whose provider cannot report run-state.
	RunStateUnmanaged RunState = "unmanaged"
)

// Cluster is a single cluster discovered from a provider.
type Cluster struct {
	Name         string
	Distribution v1alpha1.Distribution
	Provider     v1alpha1.Provider
	// RunState is the cluster's coarse running/stopped state when the provider can report it
	// (Docker today). RunStateUnknown for providers that do not — callers must not treat unknown as
	// stopped.
	RunState RunState
}

// ProviderError records a non-fatal failure while listing one provider. Discovery continues for the
// remaining providers; callers decide whether to surface these (the CLI prints warnings, the UI
// logs them).
type ProviderError struct {
	Provider v1alpha1.Provider
	Err      error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("list %s clusters: %v", e.Provider, e.Err)
}

func (e ProviderError) Unwrap() error { return e.Err }

// ClusterLister lists the cluster names managed by a single-distribution cloud provider (Hetzner,
// Omni, AWS, GCP, Azure). *hetzner.Provider, *omni.Provider, *aws.Provider, *gcp.Provider and
// *azure.Provider all satisfy it.
type ClusterLister interface {
	ListAllClusters(ctx context.Context) ([]string, error)
}

// KubernetesLister lists nested clusters together with their detected distribution.
// *kubernetes.Provider satisfies it.
type KubernetesLister interface {
	ListAllClustersWithDistribution(ctx context.Context) ([]kubernetesprovider.ClusterInfo, error)
}

// DockerFactory builds a provisioner factory for a Docker-based distribution so its local clusters
// can be enumerated. Injectable so tests can substitute fake provisioners.
type DockerFactory func(distribution v1alpha1.Distribution) (clusterprovisioner.Factory, error)

// Discoverer enumerates clusters across providers. The zero value is usable (it resolves
// credentials from the process environment and builds real providers); the optional fields override
// construction for tests.
type Discoverer struct {
	// Resolver supplies provider credentials. Nil means credentials.EnvResolver{} (process env).
	Resolver credentials.Resolver

	// The following override real provider construction; primarily for tests. A non-nil lister is
	// used as-is (its own availability is assumed); when nil, a real provider is built from Resolver
	// credentials and silently skipped when those credentials are absent.
	Hetzner       ClusterLister
	Omni          ClusterLister
	AWS           ClusterLister
	GCP           ClusterLister
	Azure         ClusterLister
	Kubernetes    KubernetesLister
	DockerFactory DockerFactory

	// LookPath resolves an executable on PATH (used to gate AWS discovery on the eksctl binary).
	// Nil means exec.LookPath. Primarily for tests.
	LookPath func(string) (string, error)

	// DockerPing probes the Docker daemon for availability reporting. Nil means a real ping via a
	// docker client built from the environment. Primarily for tests.
	DockerPing func(ctx context.Context) error

	// DockerStatus reports a Docker-based cluster's run-state (running/stopped) by distribution and
	// name, so discovery can surface a stopped cluster as not-running rather than falsely Ready. Nil
	// means a real query via a docker provider built from the environment; a probe that errors yields
	// RunStateUnknown (never a discovery failure). Primarily for tests.
	DockerStatus func(ctx context.Context, distribution v1alpha1.Distribution, name string) RunState

	// ProbeRunState gates the per-cluster Docker run-state probe in listDocker. Callers that render
	// run-state (the web UI) set it true; callers that only enumerate names (the `cluster list` CLI,
	// which does not display run-state) leave it false to avoid N wasted Docker round-trips per
	// invocation. A configured DockerStatus seam also enables probing so tests need not set this.
	ProbeRunState bool
}

// DefaultProviders is the provider set `ksail cluster list` queries when no --provider filter is
// given: the infrastructure providers that own clusters out of the box. Kubernetes (nested) and AWS
// are reachable via an explicit filter / the UI's full set.
func DefaultProviders() []v1alpha1.Provider {
	return []v1alpha1.Provider{
		v1alpha1.ProviderDocker,
		v1alpha1.ProviderHetzner,
		v1alpha1.ProviderOmni,
	}
}

// AllProviders is every provider discovery can enumerate. The local web-UI backend queries this set
// so a cluster on any provider the machine has access to becomes visible.
func AllProviders() []v1alpha1.Provider {
	return []v1alpha1.Provider{
		v1alpha1.ProviderDocker,
		v1alpha1.ProviderHetzner,
		v1alpha1.ProviderOmni,
		v1alpha1.ProviderAWS,
		v1alpha1.ProviderGCP,
		v1alpha1.ProviderAzure,
		v1alpha1.ProviderKubernetes,
	}
}

// LocalDistributions is the set of Docker-based distributions enumerated when listing local
// clusters. It is broader than the creatable set so clusters created out-of-band still appear.
func LocalDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
		v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
	}
}

// Discover enumerates clusters for the given providers concurrently and returns the discovered
// clusters together with any per-provider listing failures. Results are returned in provider order
// (and, within a provider, in the order the provider reports them) so output is deterministic
// despite the concurrency. Providers without credentials are skipped silently (no ProviderError).
func (d *Discoverer) Discover(
	ctx context.Context,
	providers []v1alpha1.Provider,
) ([]Cluster, []ProviderError) {
	type outcome struct {
		clusters []Cluster
		failure  *ProviderError
	}

	outcomes := make([]outcome, len(providers))

	var waitGroup sync.WaitGroup

	for index, prov := range providers {
		waitGroup.Add(1)

		go func(index int, prov v1alpha1.Provider) {
			defer waitGroup.Done()

			clusters, err := d.listProvider(ctx, prov)
			outcomes[index] = outcome{clusters: clusters}

			if err != nil {
				outcomes[index].failure = &ProviderError{Provider: prov, Err: err}
			}
		}(index, prov)
	}

	waitGroup.Wait()

	var (
		clusters []Cluster
		failures []ProviderError
	)

	for _, item := range outcomes {
		clusters = append(clusters, item.clusters...)

		if item.failure != nil {
			failures = append(failures, *item.failure)
		}
	}

	return clusters, failures
}

func (d *Discoverer) listProvider(
	ctx context.Context,
	prov v1alpha1.Provider,
) ([]Cluster, error) {
	switch prov {
	case v1alpha1.ProviderDocker:
		return d.listDocker(ctx)
	case v1alpha1.ProviderHetzner:
		return d.listHetzner(ctx)
	case v1alpha1.ProviderOmni:
		return d.listOmni(ctx)
	case v1alpha1.ProviderAWS:
		return d.listAWS(ctx)
	case v1alpha1.ProviderGCP:
		return d.listGCP(ctx)
	case v1alpha1.ProviderAzure:
		return d.listAzure(ctx)
	case v1alpha1.ProviderKubernetes:
		return d.listKubernetes(ctx)
	default:
		return nil, fmt.Errorf("%w: %s", clustererr.ErrUnsupportedProvider, prov)
	}
}

func (d *Discoverer) resolver() credentials.Resolver {
	if d.Resolver != nil {
		return d.Resolver
	}

	return credentials.EnvResolver{}
}

// listDocker enumerates Docker-based clusters across all local distributions, deduplicated by name
// (a name uniquely identifies a Docker cluster, and the first distribution that reports it wins).
// Per-distribution failures are swallowed (best-effort), matching `ksail cluster list`.
func (d *Discoverer) listDocker(ctx context.Context) ([]Cluster, error) {
	seen := make(map[string]struct{})

	var clusters []Cluster

	for _, distribution := range LocalDistributions() {
		factory, err := d.dockerFactory(distribution)
		if err != nil {
			continue
		}

		clusterCfg := &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{Distribution: distribution}},
		}

		provisioner, _, err := factory.Create(ctx, clusterCfg)
		if err != nil {
			continue
		}

		names, err := provisioner.List(ctx)
		if err != nil {
			continue
		}

		for _, name := range names {
			if _, ok := seen[name]; ok {
				continue
			}

			seen[name] = struct{}{}

			// Only probe run-state when a caller actually renders it (web UI). The
			// `cluster list` CLI does not display run-state, so it skips the per-cluster
			// Docker round-trip and leaves RunStateUnknown.
			runState := RunStateUnknown
			if d.ProbeRunState || d.DockerStatus != nil {
				runState = d.dockerRunState(ctx, distribution, name)
			}

			clusters = append(clusters, Cluster{
				Name:         name,
				Distribution: distribution,
				Provider:     v1alpha1.ProviderDocker,
				RunState:     runState,
			})
		}
	}

	return clusters, nil
}

func (d *Discoverer) dockerFactory(
	distribution v1alpha1.Distribution,
) (clusterprovisioner.Factory, error) {
	if d.DockerFactory != nil {
		return d.DockerFactory(distribution)
	}

	return clusterprovisioner.DefaultFactory{
		DistributionConfig: emptyDistributionConfig(distribution),
	}, nil
}

// emptyDistributionConfig builds the minimal DistributionConfig a provisioner needs to LIST
// clusters (the cluster name and node config are irrelevant for listing).
func emptyDistributionConfig(
	distribution v1alpha1.Distribution,
) *clusterprovisioner.DistributionConfig {
	switch distribution {
	case v1alpha1.DistributionK3s:
		return &clusterprovisioner.DistributionConfig{K3d: &v1alpha5.SimpleConfig{}}
	case v1alpha1.DistributionTalos:
		return &clusterprovisioner.DistributionConfig{Talos: &talosconfigmanager.Configs{}}
	case v1alpha1.DistributionVCluster:
		return &clusterprovisioner.DistributionConfig{
			VCluster: &clusterprovisioner.VClusterConfig{},
		}
	case v1alpha1.DistributionKWOK:
		return &clusterprovisioner.DistributionConfig{KWOK: &clusterprovisioner.KWOKConfig{}}
	case v1alpha1.DistributionEKS:
		return &clusterprovisioner.DistributionConfig{EKS: &clusterprovisioner.EKSConfig{}}
	case v1alpha1.DistributionGKE:
		return &clusterprovisioner.DistributionConfig{GKE: &clusterprovisioner.GKEConfig{}}
	case v1alpha1.DistributionAKS:
		return &clusterprovisioner.DistributionConfig{AKS: &clusterprovisioner.AKSConfig{}}
	case v1alpha1.DistributionVanilla:
		return &clusterprovisioner.DistributionConfig{Kind: &v1alpha4.Cluster{}}
	default:
		return &clusterprovisioner.DistributionConfig{Kind: &v1alpha4.Cluster{}}
	}
}

// clustersWithDistribution tags a list of cluster names with a fixed distribution and provider.
func clustersWithDistribution(
	names []string,
	distribution v1alpha1.Distribution,
	prov v1alpha1.Provider,
) []Cluster {
	clusters := make([]Cluster, 0, len(names))
	for _, name := range names {
		clusters = append(clusters, Cluster{Name: name, Distribution: distribution, Provider: prov})
	}

	return clusters
}

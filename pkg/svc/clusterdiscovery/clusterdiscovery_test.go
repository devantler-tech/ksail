package clusterdiscovery_test

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	kubernetesprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

// sharedCluster is a cluster name reported by more than one distribution, used to exercise dedup.
const sharedCluster = "shared"

// --- fakes ---------------------------------------------------------------

// fakeLister is a ClusterLister returning fixed names or an error.
type fakeLister struct {
	names []string
	err   error
}

func (f fakeLister) ListAllClusters(_ context.Context) ([]string, error) {
	return f.names, f.err
}

// fakeKubeLister is a KubernetesLister returning fixed ClusterInfos or an error.
type fakeKubeLister struct {
	infos []kubernetesprovider.ClusterInfo
	err   error
}

func (f fakeKubeLister) ListAllClustersWithDistribution(
	_ context.Context,
) ([]kubernetesprovider.ClusterInfo, error) {
	return f.infos, f.err
}

// fakeProvisioner is a clusterprovisioner.Provisioner whose List returns fixed names or an error.
type fakeProvisioner struct {
	clusters []string
	listErr  error
}

func (f fakeProvisioner) Create(context.Context, string) error         { return nil }
func (f fakeProvisioner) Delete(context.Context, string) error         { return nil }
func (f fakeProvisioner) Start(context.Context, string) error          { return nil }
func (f fakeProvisioner) Stop(context.Context, string) error           { return nil }
func (f fakeProvisioner) Exists(context.Context, string) (bool, error) { return false, nil }

func (f fakeProvisioner) List(context.Context) ([]string, error) {
	return f.clusters, f.listErr
}

type fakeFactory struct {
	provisioner clusterprovisioner.Provisioner
}

func (f fakeFactory) Create(
	context.Context,
	*v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.provisioner, nil, nil
}

// dockerFactoryFor returns a DockerFactory backed by per-distribution cluster names.
func dockerFactoryFor(byDist map[v1alpha1.Distribution][]string) clusterdiscovery.DockerFactory {
	return func(distribution v1alpha1.Distribution) (clusterprovisioner.Factory, error) {
		return fakeFactory{provisioner: fakeProvisioner{clusters: byDist[distribution]}}, nil
	}
}

// emptyResolver resolves every credential to "" so real cloud construction is skipped.
type emptyResolver struct{}

func (emptyResolver) Value(credentials.Key) string    { return "" }
func (emptyResolver) EnvVar(k credentials.Key) string { return credentials.DefaultEnvVar(k) }

// lookPathMissing is a LookPath that reports the binary is not installed.
func lookPathMissing(string) (string, error) { return "", errBoom }

// --- tests ---------------------------------------------------------------

func TestDiscover_DockerDedupesAndFirstDistributionWins(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{
		DockerFactory: dockerFactoryFor(map[v1alpha1.Distribution][]string{
			v1alpha1.DistributionVanilla: {sharedCluster, "vanilla-only"},
			v1alpha1.DistributionK3s:     {sharedCluster, "k3s-only"},
		}),
	}

	clusters, failures := discoverer.Discover(context.Background(),
		[]v1alpha1.Provider{v1alpha1.ProviderDocker})

	require.Empty(t, failures)
	require.Equal(t, []clusterdiscovery.Cluster{
		{
			Name:         sharedCluster,
			Distribution: v1alpha1.DistributionVanilla,
			Provider:     v1alpha1.ProviderDocker,
		},
		{
			Name:         "vanilla-only",
			Distribution: v1alpha1.DistributionVanilla,
			Provider:     v1alpha1.ProviderDocker,
		},
		{
			Name:         "k3s-only",
			Distribution: v1alpha1.DistributionK3s,
			Provider:     v1alpha1.ProviderDocker,
		},
	}, clusters)
}

func TestDiscover_DockerListErrorSkippedSilently(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{
		DockerFactory: func(v1alpha1.Distribution) (clusterprovisioner.Factory, error) {
			return fakeFactory{provisioner: fakeProvisioner{listErr: errBoom}}, nil
		},
	}

	clusters, failures := discoverer.Discover(context.Background(),
		[]v1alpha1.Provider{v1alpha1.ProviderDocker})

	assert.Empty(t, clusters)
	assert.Empty(t, failures, "per-distribution list errors are swallowed, not surfaced")
}

func TestDiscover_CloudProvidersMapToTheirDistributions(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{
		Hetzner: fakeLister{names: []string{"prod"}},
		Omni:    fakeLister{names: []string{"omni-prod"}},
		AWS:     fakeLister{names: []string{"eks-1"}},
		GCP:     fakeLister{names: []string{"gke-1"}},
		Kubernetes: fakeKubeLister{
			infos: []kubernetesprovider.ClusterInfo{{Name: "nested", Distribution: "K3s"}},
		},
	}

	clusters, failures := discoverer.Discover(context.Background(), []v1alpha1.Provider{
		v1alpha1.ProviderHetzner,
		v1alpha1.ProviderOmni,
		v1alpha1.ProviderAWS,
		v1alpha1.ProviderGCP,
		v1alpha1.ProviderKubernetes,
	})

	require.Empty(t, failures)
	require.Equal(t, []clusterdiscovery.Cluster{
		{
			Name:         "prod",
			Distribution: v1alpha1.DistributionTalos,
			Provider:     v1alpha1.ProviderHetzner,
		},
		{
			Name:         "omni-prod",
			Distribution: v1alpha1.DistributionTalos,
			Provider:     v1alpha1.ProviderOmni,
		},
		{Name: "eks-1", Distribution: v1alpha1.DistributionEKS, Provider: v1alpha1.ProviderAWS},
		{Name: "gke-1", Distribution: v1alpha1.DistributionGKE, Provider: v1alpha1.ProviderGCP},
		{
			Name:         "nested",
			Distribution: v1alpha1.DistributionK3s,
			Provider:     v1alpha1.ProviderKubernetes,
		},
	}, clusters)
}

func TestDiscover_KubernetesUnknownDistributionFallsBackToVanilla(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{
		Kubernetes: fakeKubeLister{
			infos: []kubernetesprovider.ClusterInfo{{Name: "mystery", Distribution: "NotADistro"}},
		},
	}

	clusters, _ := discoverer.Discover(context.Background(),
		[]v1alpha1.Provider{v1alpha1.ProviderKubernetes})

	require.Len(t, clusters, 1)
	assert.Equal(t, v1alpha1.DistributionVanilla, clusters[0].Distribution)
}

func TestDiscover_SkipsCloudProvidersWithoutCredentials(t *testing.T) {
	t.Parallel()

	// No injected listers + empty resolver + eksctl reported missing => every cloud provider skips
	// silently regardless of the host's real environment (GCP's project check short-circuits
	// before its host credential-file probe).
	discoverer := &clusterdiscovery.Discoverer{
		Resolver: emptyResolver{},
		LookPath: lookPathMissing,
	}

	clusters, failures := discoverer.Discover(context.Background(), []v1alpha1.Provider{
		v1alpha1.ProviderHetzner,
		v1alpha1.ProviderOmni,
		v1alpha1.ProviderAWS,
		v1alpha1.ProviderGCP,
	})

	assert.Empty(t, clusters)
	assert.Empty(t, failures, "providers without credentials are skipped, not reported as failures")
}

func TestDiscover_UnknownProviderReturnsProviderError(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{}

	clusters, failures := discoverer.Discover(context.Background(),
		[]v1alpha1.Provider{v1alpha1.Provider("Bogus")})

	assert.Empty(t, clusters)
	require.Len(t, failures, 1)
	assert.Equal(t, v1alpha1.Provider("Bogus"), failures[0].Provider)
	assert.ErrorIs(t, failures[0].Err, clustererr.ErrUnsupportedProvider)
}

func TestDiscover_SurfacesProviderFailureButKeepsOthers(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{
		Hetzner: fakeLister{err: errBoom},
		Omni:    fakeLister{names: []string{"ok"}},
	}

	clusters, failures := discoverer.Discover(context.Background(),
		[]v1alpha1.Provider{v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni})

	require.Len(t, clusters, 1)
	assert.Equal(t, "ok", clusters[0].Name)
	require.Len(t, failures, 1)
	assert.Equal(t, v1alpha1.ProviderHetzner, failures[0].Provider)
	assert.ErrorIs(t, failures[0].Err, errBoom)
}

func TestDiscover_PreservesProviderOrder(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{
		DockerFactory: dockerFactoryFor(map[v1alpha1.Distribution][]string{
			v1alpha1.DistributionVanilla: {"local"},
		}),
		Hetzner: fakeLister{names: []string{"cloud"}},
	}

	clusters, _ := discoverer.Discover(context.Background(),
		[]v1alpha1.Provider{v1alpha1.ProviderDocker, v1alpha1.ProviderHetzner})

	require.Len(t, clusters, 2)
	assert.Equal(t, v1alpha1.ProviderDocker, clusters[0].Provider)
	assert.Equal(t, v1alpha1.ProviderHetzner, clusters[1].Provider)
}

func TestDiscover_DockerReportsRunStateFromSeam(t *testing.T) {
	t.Parallel()

	discoverer := &clusterdiscovery.Discoverer{
		DockerFactory: dockerFactoryFor(map[v1alpha1.Distribution][]string{
			v1alpha1.DistributionVanilla: {"running-one", "stopped-one"},
		}),
		DockerStatus: func(
			_ context.Context, _ v1alpha1.Distribution, name string,
		) clusterdiscovery.RunState {
			if name == "stopped-one" {
				return clusterdiscovery.RunStateStopped
			}

			return clusterdiscovery.RunStateRunning
		},
	}

	clusters, failures := discoverer.Discover(context.Background(),
		[]v1alpha1.Provider{v1alpha1.ProviderDocker})

	require.Empty(t, failures)
	require.Len(t, clusters, 2)

	byName := map[string]clusterdiscovery.RunState{}
	for _, cluster := range clusters {
		byName[cluster.Name] = cluster.RunState
	}

	assert.Equal(t, clusterdiscovery.RunStateRunning, byName["running-one"])
	assert.Equal(t, clusterdiscovery.RunStateStopped, byName["stopped-one"])
}

func TestProviderSets(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []v1alpha1.Provider{
		v1alpha1.ProviderDocker, v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni,
	}, clusterdiscovery.DefaultProviders())

	assert.Subset(t, clusterdiscovery.AllProviders(), clusterdiscovery.DefaultProviders())
	assert.Contains(t, clusterdiscovery.AllProviders(), v1alpha1.ProviderAWS)
	assert.Contains(t, clusterdiscovery.AllProviders(), v1alpha1.ProviderGCP)
	assert.Contains(t, clusterdiscovery.AllProviders(), v1alpha1.ProviderKubernetes)
	assert.Len(t, clusterdiscovery.LocalDistributions(), 5)
}

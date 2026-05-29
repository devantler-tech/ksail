package clusterapi_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	eventuallyTimeout = 2 * time.Second
	eventuallyTick    = 10 * time.Millisecond
)

// Static sentinel errors used to drive provisioner failures in tests (err113 forbids inline
// errors.New at the call site).
var (
	errSimulatedCreateFailure = errors.New("simulated create failure")
	errSimulatedDeleteFailure = errors.New("docker refused to remove container")
)

// fakeProvisioner is an in-memory clusterprovisioner.Provisioner. Its List reflects the clusters it
// has created and not yet deleted, so the Service's live enumeration behaves like a real provider.
// Optional gates let tests hold Create/Delete in-flight to observe intermediate phases.
type fakeProvisioner struct {
	mu         sync.Mutex
	clusters   []string
	createGate chan struct{}
	deleteGate chan struct{}
	createErr  error
	deleteErr  error
	created    []string
	deleted    []string
}

func (f *fakeProvisioner) Create(_ context.Context, name string) error {
	if f.createGate != nil {
		<-f.createGate
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.created = append(f.created, name)

	if f.createErr != nil {
		return f.createErr
	}

	f.clusters = append(f.clusters, name)

	return nil
}

func (f *fakeProvisioner) Delete(_ context.Context, name string) error {
	if f.deleteGate != nil {
		<-f.deleteGate
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleted = append(f.deleted, name)

	if f.deleteErr != nil {
		return f.deleteErr
	}

	// Mirror the real provisioners (e.g. Kind): deleting a cluster that does not exist returns
	// ErrClusterNotFound rather than silently succeeding.
	if !slices.Contains(f.clusters, name) {
		return clustererr.ErrClusterNotFound
	}

	f.clusters = slices.DeleteFunc(f.clusters, func(existing string) bool {
		return existing == name
	})

	return nil
}

func (f *fakeProvisioner) List(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.clusters), nil
}

func (f *fakeProvisioner) Start(_ context.Context, _ string) error { return nil }
func (f *fakeProvisioner) Stop(_ context.Context, _ string) error  { return nil }

func (f *fakeProvisioner) Exists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Contains(f.clusters, name), nil
}

func (f *fakeProvisioner) createdNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.created)
}

func (f *fakeProvisioner) deletedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.deleted)
}

type fakeFactory struct {
	provisioner clusterprovisioner.Provisioner
}

func (f fakeFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.provisioner, nil, nil
}

// newTestService wires a Service whose factory routes each distribution to a supplied provisioner.
// Distributions without an entry get a shared empty provisioner.
func newTestService(byDistribution map[v1alpha1.Distribution]*fakeProvisioner) *clusterapi.Service {
	empty := &fakeProvisioner{}

	return clusterapi.NewTestService(func(
		distribution v1alpha1.Distribution,
		_ string,
	) (clusterprovisioner.Factory, error) {
		provisioner, ok := byDistribution[distribution]
		if !ok {
			provisioner = empty
		}

		return fakeFactory{provisioner: provisioner}, nil
	})
}

func clusterFor(name string, distribution v1alpha1.Distribution) *v1alpha1.Cluster {
	cluster := &v1alpha1.Cluster{}
	cluster.Name = name
	cluster.Spec.Cluster.Distribution = distribution

	return cluster
}

func phaseOf(list *v1alpha1.ClusterList, name string) (v1alpha1.ClusterPhase, bool) {
	for i := range list.Items {
		if list.Items[i].Name == name {
			return list.Items[i].Status.Phase, true
		}
	}

	return "", false
}

func TestListMapsExistingClusters(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{"dev"}},
	})

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	got := list.Items[0]
	assert.Equal(t, "dev", got.Name)
	assert.Equal(t, "default", got.Namespace)
	assert.Equal(t, v1alpha1.DistributionVanilla, got.Spec.Cluster.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, got.Spec.Cluster.Provider)
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
}

func TestListEmptyReturnsNonNilItems(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	list, err := service.List(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, list.Items)
	assert.Empty(t, list.Items)
}

func TestCreateIsAsyncAndTransitionsToReady(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	provisioner := &fakeProvisioner{createGate: gate}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVCluster: provisioner,
	})

	created, err := service.Create(
		context.Background(),
		clusterFor("new", v1alpha1.DistributionVCluster),
	)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.ClusterPhaseProvisioning, created.Status.Phase)

	// While the create goroutine is gated, the cluster reports Provisioning.
	list, err := service.List(context.Background())
	require.NoError(t, err)

	phase, ok := phaseOf(list, "new")
	require.True(t, ok)
	assert.Equal(t, v1alpha1.ClusterPhaseProvisioning, phase)

	close(gate)

	require.Eventually(t, func() bool {
		current, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		readyPhase, found := phaseOf(current, "new")

		return found && readyPhase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)

	assert.Equal(t, []string{"new"}, provisioner.createdNames())
}

func TestDeleteIsAsyncAndRemovesCluster(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	provisioner := &fakeProvisioner{clusters: []string{"old"}, deleteGate: gate}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVCluster: provisioner,
	})

	require.NoError(t, service.Delete(context.Background(), "default", "old"))

	// While the delete goroutine is gated, the cluster reports Deleting.
	list, err := service.List(context.Background())
	require.NoError(t, err)

	phase, ok := phaseOf(list, "old")
	require.True(t, ok)
	assert.Equal(t, v1alpha1.ClusterPhaseDeleting, phase)

	close(gate)

	require.Eventually(t, func() bool {
		current, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		_, present := phaseOf(current, "old")

		return !present
	}, eventuallyTimeout, eventuallyTick)

	assert.Equal(t, []string{"old"}, provisioner.deletedNames())
}

// TestDeleteClearsFailedClusterWithNoUnderlyingCluster reproduces the bug where a cluster left in
// the Failed phase by a failed create could never be removed from the web UI: deleting it called
// the provisioner's Delete, which returned ErrClusterNotFound (there is no cluster to delete), and
// the entry was pinned Failed forever. Deleting must instead be idempotent and clear the entry.
func TestDeleteClearsFailedClusterWithNoUnderlyingCluster(t *testing.T) {
	t.Parallel()

	// createErr makes the background create fail, leaving "broken" tracked as Failed with no live
	// cluster behind it (List/Exists report it absent, exactly like a half-finished Kind create).
	provisioner := &fakeProvisioner{createErr: errSimulatedCreateFailure}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: provisioner,
	})

	_, err := service.Create(
		context.Background(),
		clusterFor("broken", v1alpha1.DistributionVanilla),
	)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, "broken")

		return ok && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	// Deleting the Failed cluster must clear it from the list, not re-pin it as Failed.
	require.NoError(t, service.Delete(context.Background(), "default", "broken"))

	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		_, present := phaseOf(list, "broken")

		return !present
	}, eventuallyTimeout, eventuallyTick)
}

// TestDeleteKeepsClusterFailedWhenDeletionErrors ensures a genuine deletion failure (the cluster
// exists but Delete errors) still surfaces as Failed, so real problems are not hidden by the
// idempotent "already gone" handling above.
func TestDeleteKeepsClusterFailedWhenDeletionErrors(t *testing.T) {
	t.Parallel()

	provisioner := &fakeProvisioner{
		clusters:  []string{"stuck"},
		deleteErr: errSimulatedDeleteFailure,
	}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: provisioner,
	})

	require.NoError(t, service.Delete(context.Background(), "default", "stuck"))

	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, "stuck")

		return ok && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)
}

func TestCreateValidatesInput(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	_, err := service.Create(context.Background(), clusterFor("", v1alpha1.DistributionVCluster))
	require.ErrorIs(t, err, api.ErrInvalid)

	_, err = service.Create(context.Background(), clusterFor("noDist", ""))
	require.ErrorIs(t, err, api.ErrInvalid)

	// An unknown distribution cannot be provisioned locally.
	_, err = service.Create(
		context.Background(),
		clusterFor("bogus", v1alpha1.Distribution("Bogus")),
	)
	require.ErrorIs(t, err, api.ErrNotSupported)

	// An invalid (distribution, provider) combination is rejected before any work is enqueued, so the
	// provisioner can never silently provision a backend that disagrees with the requested provider.
	eksOnDocker := clusterFor("eks-on-docker", v1alpha1.DistributionEKS)
	eksOnDocker.Spec.Cluster.Provider = v1alpha1.ProviderDocker
	_, err = service.Create(context.Background(), eksOnDocker)
	require.ErrorIs(t, err, api.ErrInvalid)
}

func TestCreateRejectsExistingCluster(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVCluster: {clusters: []string{"dup"}},
	})

	_, err := service.Create(context.Background(), clusterFor("dup", v1alpha1.DistributionVCluster))
	require.ErrorIs(t, err, api.ErrAlreadyExists)
}

func TestUpdateIsNotSupported(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	_, err := service.Update(
		context.Background(),
		"default",
		"x",
		clusterFor("x", v1alpha1.DistributionVCluster),
	)
	require.ErrorIs(t, err, api.ErrNotSupported)
}

func TestGetIgnoresNamespaceAndReturnsNotFound(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionK3s: {clusters: []string{"only"}},
	})

	got, err := service.Get(context.Background(), "anything", "only")
	require.NoError(t, err)
	assert.Equal(t, "only", got.Name)
	assert.Equal(t, "default", got.Namespace)

	_, err = service.Get(context.Background(), "default", "missing")
	require.ErrorIs(t, err, api.ErrNotFound)
}

func TestDeleteUnknownClusterReturnsNotFound(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	err := service.Delete(context.Background(), "default", "ghost")
	require.ErrorIs(t, err, api.ErrNotFound)
}

func TestCreatableDistributions(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{"Vanilla", "K3s", "Talos", "VCluster", "KWOK", "EKS"},
		clusterapi.CreatableDistributions(),
	)
}

// recordingFactory captures the cluster the provisioner factory is asked to build, but only for the
// create call (which sets a non-empty provider) — discovery's enumerate calls leave it empty.
type recordingFactory struct {
	sink chan<- *v1alpha1.Cluster
}

func (f recordingFactory) Create(
	_ context.Context,
	cluster *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	if cluster.Spec.Cluster.Provider != "" {
		select {
		case f.sink <- cluster.DeepCopy():
		default:
		}
	}

	return &fakeProvisioner{}, nil, nil
}

// TestEKSConfigForCreate writes a region-stamped eks.yaml under ~/.ksail so the EKS provisioner has
// the on-disk config it requires. The region comes from AWS_REGION (which Settings/overlay set).
func TestEKSConfigForCreate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-central-1")

	configPath, region, err := clusterapi.ExportEKSConfigForCreate("prod")
	require.NoError(t, err)
	assert.Equal(t, "eu-central-1", region)
	assert.FileExists(t, configPath)

	data, err := os.ReadFile(configPath) //nolint:gosec // test-controlled path under a temp HOME.
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: prod")
	assert.Contains(t, string(data), "region: eu-central-1")
}

// TestEKSConfigRejectsNonSegmentName guards the path-traversal hardening: the cluster name becomes a
// single directory under ~/.ksail/clusters, so names containing separators or the "."/".." specials
// must be rejected rather than redirecting the write into an unintended directory.
func TestEKSConfigRejectsNonSegmentName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"foo/bar", ".", "..", "../escape", "/abs"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := clusterapi.ExportEKSConfigForCreate(name)
			require.ErrorIs(t, err, api.ErrInvalid)
		})
	}
}

// TestCreatePassesProviderToFactory is the Phase 4 regression guard: the create path must route the
// requested provider (and distribution) to the factory, so a Talos/Hetzner request provisions on
// Hetzner rather than silently falling back to local Docker.
func TestCreatePassesProviderToFactory(t *testing.T) {
	t.Parallel()

	captured := make(chan *v1alpha1.Cluster, 1)
	service := clusterapi.NewTestService(
		func(_ v1alpha1.Distribution, _ string) (clusterprovisioner.Factory, error) {
			return recordingFactory{sink: captured}, nil
		},
	)

	cluster := clusterFor("prod", v1alpha1.DistributionTalos)
	cluster.Spec.Cluster.Provider = v1alpha1.ProviderHetzner

	_, err := service.Create(context.Background(), cluster)
	require.NoError(t, err)

	select {
	case built := <-captured:
		assert.Equal(t, v1alpha1.DistributionTalos, built.Spec.Cluster.Distribution)
		assert.Equal(t, v1alpha1.ProviderHetzner, built.Spec.Cluster.Provider)
	case <-time.After(eventuallyTimeout):
		t.Fatal("factory was never asked to build the requested Talos/Hetzner cluster")
	}
}

// TestCreateDefaultsEKSProviderToAWS guards the provider-defaulting fix: an EKS create request with
// no explicit provider must default to AWS (not the global Docker default), so the returned cluster
// is labelled AWS and the factory is asked to provision the AWS backend.
func TestCreateDefaultsEKSProviderToAWS(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-central-1")

	captured := make(chan *v1alpha1.Cluster, 1)
	service := clusterapi.NewTestService(
		func(_ v1alpha1.Distribution, _ string) (clusterprovisioner.Factory, error) {
			return recordingFactory{sink: captured}, nil
		},
	)

	// Provider intentionally left empty — Create must default it to AWS for EKS.
	created, err := service.Create(
		context.Background(),
		clusterFor("prod-eks", v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.ProviderAWS, created.Spec.Cluster.Provider,
		"an EKS request without a provider must default to AWS")

	select {
	case built := <-captured:
		assert.Equal(t, v1alpha1.ProviderAWS, built.Spec.Cluster.Provider)
	case <-time.After(eventuallyTimeout):
		t.Fatal("factory was never asked to build the EKS cluster with a defaulted AWS provider")
	}
}

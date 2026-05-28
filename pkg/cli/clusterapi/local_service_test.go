package clusterapi_test

import (
	"context"
	"errors"
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

	_, err = service.Create(context.Background(), clusterFor("talos", v1alpha1.DistributionTalos))
	require.ErrorIs(t, err, api.ErrNotSupported)
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

	assert.Equal(t, []string{"Vanilla", "K3s", "VCluster"}, clusterapi.CreatableDistributions())
}

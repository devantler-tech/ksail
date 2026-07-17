package clusterapi_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	eventuallyTimeout = 2 * time.Second
	eventuallyTick    = 10 * time.Millisecond

	// devClusterName is the discovered-cluster name shared by the List tests.
	devClusterName = "dev"
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
	startGate  chan struct{}
	stopGate   chan struct{}
	createErr  error
	deleteErr  error
	startErr   error
	stopErr    error
	created    []string
	deleted    []string
	started    []string
	stopped    []string
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

func (f *fakeProvisioner) Start(_ context.Context, name string) error {
	if f.startGate != nil {
		<-f.startGate
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.started = append(f.started, name)

	return f.startErr
}

func (f *fakeProvisioner) Stop(_ context.Context, name string) error {
	if f.stopGate != nil {
		<-f.stopGate
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.stopped = append(f.stopped, name)

	return f.stopErr
}

func (f *fakeProvisioner) Exists(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Contains(f.clusters, name), nil
}

func (f *fakeProvisioner) startedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.started)
}

func (f *fakeProvisioner) stoppedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.stopped)
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
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	got := list.Items[0]
	assert.Equal(t, devClusterName, got.Name)
	assert.Equal(t, "default", got.Namespace)
	assert.Equal(t, v1alpha1.DistributionVanilla, got.Spec.Cluster.Distribution)
	assert.Equal(t, v1alpha1.ProviderDocker, got.Spec.Cluster.Provider)
	assert.Equal(t, v1alpha1.ClusterPhaseReady, got.Status.Phase)
}

// TestListReportsStoppedClusterAsNotReady guards 5.7: a discovered Docker cluster whose run-state is
// Stopped reports the ClusterPhaseStopped phase (so the web UI renders it distinctly, not green) and
// also carries the backward-compatible Ready=False/reason=Stopped condition for consumers predating
// the Stopped phase value.
func TestListReportsStoppedClusterAsNotReady(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})
	service.SetDockerStatusForTest(func(
		context.Context, v1alpha1.Distribution, string,
	) clusterdiscovery.RunState {
		return clusterdiscovery.RunStateStopped
	})

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	got := list.Items[0]
	assert.Equal(t, v1alpha1.ClusterPhaseStopped, got.Status.Phase,
		"a stopped cluster reports the Stopped phase, not Ready")

	conditions := conditionsOf(list, devClusterName)
	require.Len(t, conditions, 1)
	assert.Equal(t, "Ready", conditions[0].Type)
	assert.Equal(t, metav1.ConditionFalse, conditions[0].Status)
	assert.Equal(t, "Stopped", conditions[0].Reason)
}

// TestListReportsRunningClusterAsReady pins that an explicitly-running run-state keeps the cluster
// Ready with no synthetic condition (the common case must be unchanged by the stopped handling).
func TestListReportsRunningClusterAsReady(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})
	service.SetDockerStatusForTest(func(
		context.Context, v1alpha1.Distribution, string,
	) clusterdiscovery.RunState {
		return clusterdiscovery.RunStateRunning
	})

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	assert.Equal(t, v1alpha1.ClusterPhaseReady, list.Items[0].Status.Phase)
	assert.Empty(t, conditionsOf(list, devClusterName))
}

// TestListReportsEndpointFromKubeconfig guards the local status enrichment: a discovered cluster
// whose kubeconfig context is detectable by name must report that context's API server URL as
// status.endpoint, so the web UI's Status card shows a real endpoint on the local surface.
func TestListReportsEndpointFromKubeconfig(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})

	kubeconfig := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(kubeconfig, []byte(`apiVersion: v1
kind: Config
clusters:
- name: kind-dev
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: kind-dev
  context:
    cluster: kind-dev
    user: kind-dev
users:
- name: kind-dev
  user: {}
`), 0o600))
	service.SetKubeconfigPathForTest(kubeconfig)

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "https://127.0.0.1:6443", list.Items[0].Status.Endpoint)
}

// TestListWithoutKubeconfigLeavesEndpointEmpty covers the best-effort path: no kubeconfig means no
// endpoint, never an error.
func TestListWithoutKubeconfigLeavesEndpointEmpty(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Empty(t, list.Items[0].Status.Endpoint)
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

func TestDeleteEKSClearsPersistedOwnershipAndCapacityState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "old-eks"

	provisioner := &fakeProvisioner{}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: provisioner,
	})
	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)

	require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
	}))
	require.NoError(t, state.SaveEKSNodegroupState(clusterName, &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		Nodegroups: []state.EKSNodegroupCapacity{
			{Name: "workers", DesiredCapacity: 2, MinSize: 1, MaxSize: 3},
		},
	}))

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		_, specErr := state.LoadClusterSpec(clusterName)
		_, capacityErr := state.LoadEKSNodegroupState(clusterName)

		return errors.Is(specErr, state.ErrStateNotFound) &&
			errors.Is(capacityErr, state.ErrEKSNodegroupStateNotFound)
	}, eventuallyTimeout, eventuallyTick)
}

func TestDeleteEKSRejectsOverlappingCreate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "creating-eks"

	createGate := make(chan struct{})
	provisioner := &fakeProvisioner{createGate: createGate}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: provisioner,
	})
	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)

	err = service.Delete(context.Background(), "default", clusterName)
	require.ErrorIs(t, err, api.ErrAlreadyExists)
	assert.Empty(t, provisioner.deletedNames())

	close(createGate)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)

	persisted, loadErr := state.LoadClusterSpec(clusterName)
	require.NoError(t, loadErr)
	assert.Equal(t, v1alpha1.DistributionEKS, persisted.Distribution)
	assert.Equal(t, v1alpha1.ProviderAWS, persisted.Provider)
}

func TestLifecycleRejectsOverlappingOperation(t *testing.T) {
	t.Parallel()

	const clusterName = "busy"

	stopGate := make(chan struct{})
	provisioner := &fakeProvisioner{clusters: []string{clusterName}, stopGate: stopGate}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVCluster: provisioner,
	})

	require.NoError(t, service.Stop(context.Background(), "default", clusterName))
	err := service.Start(context.Background(), "default", clusterName)
	require.ErrorIs(t, err, api.ErrAlreadyExists)
	assert.Empty(t, provisioner.startedNames())

	close(stopGate)
	require.Eventually(t, func() bool {
		return slices.Equal(provisioner.stoppedNames(), []string{clusterName})
	}, eventuallyTimeout, eventuallyTick)
}

func TestDeleteEKSFailurePreservesPersistedOwnershipAndCapacityState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "stuck-eks"

	provisioner := &fakeProvisioner{deleteErr: errSimulatedDeleteFailure}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: provisioner,
	})
	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)
	require.NoError(t, state.SaveEKSNodegroupState(clusterName, &state.EKSNodegroupState{
		Version:     state.EKSNodegroupStateVersion,
		ClusterName: clusterName,
		Region:      "eu-north-1",
		Nodegroups: []state.EKSNodegroupCapacity{
			{Name: "workers", DesiredCapacity: 2, MinSize: 1, MaxSize: 3},
		},
	}))

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	_, specErr := state.LoadClusterSpec(clusterName)
	require.NoError(t, specErr)

	_, capacityErr := state.LoadEKSNodegroupState(clusterName)
	require.NoError(t, capacityErr)
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

// conditionsOf returns the status conditions of the named cluster in a list, or nil if absent.
func conditionsOf(list *v1alpha1.ClusterList, name string) []metav1.Condition {
	for i := range list.Items {
		if list.Items[i].Name == name {
			return list.Items[i].Status.Conditions
		}
	}

	return nil
}

func TestCreateFailureSurfacesReasonInCondition(t *testing.T) {
	t.Parallel()

	provisioner := &fakeProvisioner{createErr: errSimulatedCreateFailure}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVCluster: provisioner,
	})

	_, err := service.Create(
		context.Background(),
		clusterFor("boom", v1alpha1.DistributionVCluster),
	)
	require.NoError(t, err)

	var conditions []metav1.Condition

	// A failed create must surface its reason on the cluster's conditions so the UI can show why,
	// rather than a bare "Failed" with no detail.
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, "boom")
		if !ok || phase != v1alpha1.ClusterPhaseFailed {
			return false
		}

		conditions = conditionsOf(list, "boom")

		return len(conditions) > 0
	}, eventuallyTimeout, eventuallyTick)

	require.Len(t, conditions, 1)
	assert.Equal(t, "Error", conditions[0].Reason)
	assert.Contains(t, conditions[0].Message, errSimulatedCreateFailure.Error())
}

func TestProvisioningSurfacesProgressCondition(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	provisioner := &fakeProvisioner{createGate: gate}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVCluster: provisioner,
	})

	_, err := service.Create(
		context.Background(),
		clusterFor("wip", v1alpha1.DistributionVCluster),
	)
	require.NoError(t, err)

	// While the create goroutine is gated, the cluster carries a Provisioning condition.
	list, err := service.List(context.Background())
	require.NoError(t, err)

	conditions := conditionsOf(list, "wip")
	require.Len(t, conditions, 1)
	assert.Equal(t, "Provisioning", conditions[0].Reason)

	close(gate)
}

func TestCreateValidatesInput(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	_, err := service.Create(context.Background(), clusterFor("", v1alpha1.DistributionVCluster))
	require.ErrorIs(t, err, api.ErrInvalid)

	// A name that is a safe path component but not DNS-1123 (uppercase, underscore) is rejected at the
	// trust boundary, matching `ksail project init` and blocking path-traversal-shaped names.
	_, err = service.Create(
		context.Background(),
		clusterFor("Invalid_Name", v1alpha1.DistributionVCluster),
	)
	require.ErrorIs(t, err, api.ErrInvalid)

	_, err = service.Create(context.Background(), clusterFor("no-dist", ""))
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

// TestLocalServiceDoesNotImplementClusterUpdater documents that the local backend deliberately does
// NOT implement api.ClusterUpdater: a local cluster's configuration is managed via the CLI/files, not
// the API. The server derives capabilities.clusterUpdate=false from this and returns 501 for a PUT
// (asserted at the HTTP layer in the api package), so the SPA hides the edit affordance.
func TestLocalServiceDoesNotImplementClusterUpdater(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	_, ok := any(service).(api.ClusterUpdater)
	assert.False(t, ok, "the local backend must not advertise in-place cluster update")
}

// TestLocalServiceReportsComponentsInstallFalse documents 4.4a: the local backend implements
// api.ComponentInstaller but reports false (it does not yet run the component pipeline), so the
// server advertises componentsInstall=false and the SPA hides the create form's component selectors
// rather than offering options this backend drops.
func TestLocalServiceReportsComponentsInstallFalse(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	installer, ok := any(service).(api.ComponentInstaller)
	require.True(t, ok, "the local backend must advertise the ComponentInstaller capability marker")
	assert.False(t, installer.InstallsComponents(),
		"the local backend does not install components yet, so it must report false")
}

// TestStartIsAsyncAndInvokesProvisioner covers 4.4c's start endpoint: Start marks the cluster
// Updating, runs the provisioner's Start in the background, and clears the job on success.
func TestStartIsAsyncAndInvokesProvisioner(t *testing.T) {
	t.Parallel()

	provisioner := &fakeProvisioner{clusters: []string{"stopped"}}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: provisioner,
	})

	require.NoError(t, service.Start(context.Background(), "default", "stopped"))

	require.Eventually(t, func() bool {
		return len(provisioner.startedNames()) == 1
	}, eventuallyTimeout, eventuallyTick)

	assert.Equal(t, []string{"stopped"}, provisioner.startedNames())
}

// TestStopIsAsyncAndInvokesProvisioner covers 4.4c's stop endpoint: Stop runs the provisioner's Stop
// for the targeted cluster.
func TestStopIsAsyncAndInvokesProvisioner(t *testing.T) {
	t.Parallel()

	provisioner := &fakeProvisioner{clusters: []string{"running"}}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: provisioner,
	})

	require.NoError(t, service.Stop(context.Background(), "default", "running"))

	require.Eventually(t, func() bool {
		return len(provisioner.stoppedNames()) == 1
	}, eventuallyTimeout, eventuallyTick)

	assert.Equal(t, []string{"running"}, provisioner.stoppedNames())
}

// TestStartUnknownClusterReturnsNotFound pins that start/stop of a cluster the backend cannot resolve
// is a not-found error, never a silent no-op.
func TestStartUnknownClusterReturnsNotFound(t *testing.T) {
	t.Parallel()

	service := newTestService(nil)

	require.ErrorIs(t,
		service.Start(context.Background(), "default", "ghost"), api.ErrNotFound)
	require.ErrorIs(t,
		service.Stop(context.Background(), "default", "ghost"), api.ErrNotFound)
}

// TestStopFailureSurfacesReasonInCondition pins that a failed stop pins the cluster Failed and
// surfaces the provisioner's error on the condition, like create/delete failures do.
func TestStopFailureSurfacesReasonInCondition(t *testing.T) {
	t.Parallel()

	provisioner := &fakeProvisioner{
		clusters: []string{"jammed"},
		stopErr:  errSimulatedDeleteFailure,
	}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: provisioner,
	})

	require.NoError(t, service.Stop(context.Background(), "default", "jammed"))

	var conditions []metav1.Condition

	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, "jammed")
		if !ok || phase != v1alpha1.ClusterPhaseFailed {
			return false
		}

		conditions = conditionsOf(list, "jammed")

		return len(conditions) > 0
	}, eventuallyTimeout, eventuallyTick)

	require.Len(t, conditions, 1)
	assert.Equal(t, "Error", conditions[0].Reason)
	assert.Contains(t, conditions[0].Message, errSimulatedDeleteFailure.Error())
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

	var persisted *v1alpha1.ClusterSpec

	require.Eventually(t, func() bool {
		var loadErr error

		persisted, loadErr = state.LoadClusterSpec("prod-eks")

		return loadErr == nil
	}, eventuallyTimeout, eventuallyTick)
	require.NotNil(t, persisted)
	assert.Equal(t, v1alpha1.DistributionEKS, persisted.Distribution)
	assert.Equal(t, v1alpha1.ProviderAWS, persisted.Provider)
}

func TestCreateEKSFailureDoesNotPersistOwnershipState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "failed-eks"

	provisioner := &fakeProvisioner{createErr: errSimulatedCreateFailure}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: provisioner,
	})
	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	_, loadErr := state.LoadClusterSpec(clusterName)
	require.ErrorIs(t, loadErr, state.ErrStateNotFound)
}

func TestCreateEKSOwnershipPersistenceFailureMarksJobFailed(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "home-file")
	require.NoError(t, os.WriteFile(homeFile, []byte("not a directory"), 0o600))
	t.Setenv("HOME", homeFile)

	const clusterName = "untracked-eks"

	provisioner := &fakeProvisioner{}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: provisioner,
	})
	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		cluster := clusterNamed(list, clusterName)

		return cluster != nil && cluster.Status.Phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	list, listErr := service.List(context.Background())
	require.NoError(t, listErr)

	conditions := conditionsOf(list, clusterName)
	require.Len(t, conditions, 1)
	assert.Contains(t, conditions[0].Message, "persist local EKS cluster ownership state")
}

// clusterNamed returns a pointer to the cluster with the given name in the list, or nil if absent.
func clusterNamed(list *v1alpha1.ClusterList, name string) *v1alpha1.Cluster {
	for i := range list.Items {
		if list.Items[i].Name == name {
			return &list.Items[i]
		}
	}

	return nil
}

// TestListSurfacesUnmanagedKubeconfigContexts checks that List surfaces a kubeconfig context ksail did
// not provision as an unmanaged cluster (marked, with its endpoint), while a context that maps to a
// discovered cluster is listed once and never re-surfaced as unmanaged.
func TestListSurfacesUnmanagedKubeconfigContexts(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})

	kubeconfig := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(kubeconfig, []byte(`apiVersion: v1
kind: Config
clusters:
- name: kind-dev
  cluster:
    server: https://127.0.0.1:6443
- name: colleague
  cluster:
    server: https://cluster.example.com:6443
contexts:
- name: kind-dev
  context:
    cluster: kind-dev
    user: kind-dev
- name: colleague-cluster
  context:
    cluster: colleague
    user: colleague
users:
- name: kind-dev
  user: {}
- name: colleague
  user: {}
`), 0o600))
	service.SetKubeconfigPathForTest(kubeconfig)

	list, err := service.List(context.Background())
	require.NoError(t, err)

	// The managed cluster (context kind-dev detects to the discovered "dev") is listed once and is not
	// re-surfaced as unmanaged.
	managed := clusterNamed(list, devClusterName)
	require.NotNil(t, managed, "the discovered cluster must still be listed")
	assert.False(t, managed.IsUnmanaged(), "a discovered cluster must not be flagged unmanaged")

	// The kubeconfig-only context is surfaced, clearly marked unmanaged, with its endpoint.
	unmanaged := clusterNamed(list, "colleague-cluster")
	require.NotNil(t, unmanaged, "an unmanaged kubeconfig context must be surfaced")
	assert.True(t, unmanaged.IsUnmanaged())
	assert.Equal(t, "true", unmanaged.Annotations[v1alpha1.UnmanagedAnnotation])
	assert.Equal(t, "https://cluster.example.com:6443", unmanaged.Status.Endpoint)
	assert.Empty(t, unmanaged.Spec.Cluster.Distribution,
		"an unmanaged cluster has no ksail-known distribution")

	require.Len(t, unmanaged.Status.Conditions, 1)
	condition := unmanaged.Status.Conditions[0]
	assert.Equal(t, "Ready", condition.Type)
	assert.Equal(t, metav1.ConditionFalse, condition.Status)
	assert.Equal(t, "Unmanaged", condition.Reason)
}

// TestListSortsManagedAndUnmanagedGlobally checks that the merged managed+unmanaged list is sorted by
// name as a whole, not just within each block — an unmanaged kubeconfig context alphabetically before
// the managed cluster appears before it, and one after appears after it. Without the global sort the
// unmanaged block is simply appended after the managed block, so an earlier-sorting unmanaged cluster
// would wrongly trail a later-sorting managed one.
func TestListSortsManagedAndUnmanagedGlobally(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})

	// The managed cluster is "dev"; add unmanaged contexts on either alphabetical side of it.
	kubeconfig := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(kubeconfig, []byte(`apiVersion: v1
kind: Config
clusters:
- name: kind-dev
  cluster:
    server: https://127.0.0.1:6443
- name: aaa
  cluster:
    server: https://aaa.example.com:6443
- name: zzz
  cluster:
    server: https://zzz.example.com:6443
contexts:
- name: kind-dev
  context:
    cluster: kind-dev
    user: kind-dev
- name: aaa-cluster
  context:
    cluster: aaa
    user: aaa
- name: zzz-cluster
  context:
    cluster: zzz
    user: zzz
users:
- name: kind-dev
  user: {}
- name: aaa
  user: {}
- name: zzz
  user: {}
`), 0o600))
	service.SetKubeconfigPathForTest(kubeconfig)

	list, err := service.List(context.Background())
	require.NoError(t, err)

	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}

	// "aaa-cluster" (unmanaged) < "dev" (managed) < "zzz-cluster" (unmanaged): the combined list is
	// globally alphabetical, not the managed block followed by the unmanaged block.
	assert.Equal(t, []string{"aaa-cluster", devClusterName, "zzz-cluster"}, names)
}

// TestListWithoutKubeconfigSurfacesNoUnmanaged checks that when no kubeconfig is readable, List
// synthesizes no unmanaged clusters (newTestService points the kubeconfig at nowhere).
func TestListWithoutKubeconfigSurfacesNoUnmanaged(t *testing.T) {
	t.Parallel()

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: {clusters: []string{devClusterName}},
	})

	list, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.False(t, list.Items[0].IsUnmanaged())
}

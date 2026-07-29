package clusterapi_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/testutil/rootcheck"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
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

	// testEKSRegion is the region the EKS capacity-snapshot tests save and load under.
	testEKSRegion = "eu-north-1"
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
	saveEKSCapacitySnapshot(t, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		_, specErr := state.LoadClusterSpec(clusterName)
		_, capacityErr := state.LoadEKSNodegroupState(clusterName, testEKSRegion)

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
	saveEKSCapacitySnapshot(t, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	_, specErr := state.LoadClusterSpec(clusterName)
	require.NoError(t, specErr)

	_, capacityErr := state.LoadEKSNodegroupState(clusterName, testEKSRegion)
	require.NoError(t, capacityErr)
}

// saveEKSCapacitySnapshot writes a stop-time capacity snapshot for the cluster in the region the
// EKS delete tests use, so a test can assert whether deletion cleaned it up.
func saveEKSCapacitySnapshot(t *testing.T, clusterName string) {
	t.Helper()

	require.NoError(t, state.SaveEKSNodegroupState(clusterName, testEKSRegion,
		&state.EKSNodegroupState{
			Version:     state.EKSNodegroupStateVersion,
			ClusterName: clusterName,
			Region:      testEKSRegion,
			Nodegroups: []state.EKSNodegroupCapacity{
				{Name: "workers", DesiredCapacity: 2, MinSize: 1, MaxSize: 3},
			},
		}))
}

// TestDeleteEKSClearsJobWhenOnlyLocalStateCleanupFails covers the split between the cloud
// operation and local bookkeeping: once the EKS cluster is actually gone, a failure to remove the
// local state directory (read-only home, permissions) must not pin the job Failed. That would leave
// an undismissable row in the web UI for a cluster that no longer exists — the very trap the
// idempotent-delete behaviour exists to avoid. The cleanup failure is a warning, not a job failure.
func TestDeleteEKSClearsJobWhenOnlyLocalStateCleanupFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const clusterName = "cleanup-fails"

	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: {},
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

	// Force local state cleanup to fail deterministically while leaving the state itself readable:
	// dropping write permission on the parent directory keeps the ownership state loadable (delete
	// binds its target from it) but makes removing the cluster's state directory fail. Blanking HOME
	// would instead make the state unreadable, which the EKS mutation guard rejects up front — a
	// different failure than the cleanup-only one this test pins.
	if rootcheck.IsRootUser() {
		t.Skip("root bypasses directory permissions, so cleanup cannot be made to fail")
	}

	clustersDir := filepath.Join(home, ".ksail", "clusters")
	//nolint:gosec // 0500 is a directory mode: it keeps the state readable while removing write.
	require.NoError(t, os.Chmod(clustersDir, 0o500))

	t.Cleanup(func() {
		//nolint:gosec // 0700 is a directory mode: restores write so TempDir cleanup can remove it.
		_ = os.Chmod(clustersDir, 0o700)
	})

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		_, found := phaseOf(list, clusterName)

		return !found
	}, eventuallyTimeout, eventuallyTick)
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
			assert.NotContains(t, err.Error(), "no AWS region is selected",
				"the name must be what is rejected, not the ambient region")
		})
	}
}

// TestEKSCreateBindsTheRegionItWrites is the invariant the removed refusal was reaching for, stated
// directly. The hazard was never the empty environment variable: it was the bound region and
// metadata.region disagreeing, because boundEKSConfig compares them and rejects the file
// permanently once persisted state exists — a cluster that can never be deleted, started or
// stopped through KSail.
//
// Refusing the create only avoided that by removing a supported path. Resolving one value and
// using it on both sides prevents the disagreement outright, so this asserts the agreement in both
// environments rather than the refusal in one.
func TestEKSCreateBindsTheRegionItWrites(t *testing.T) {
	for _, env := range []struct{ name, region string }{
		{"region set", "eu-central-1"},
		{"region unset", ""},
	} {
		t.Run(env.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("AWS_REGION", env.region)

			const clusterName = "bound-region-eks"

			configPath, bound, err := clusterapi.ExportEKSConfigForCreate(clusterName)
			require.NoError(t, err)
			require.NotEmpty(t, bound, "a bound region must never be empty")

			//nolint:gosec // test-controlled path under a temp HOME.
			data, readErr := os.ReadFile(configPath)
			require.NoError(t, readErr)
			assert.Contains(t, string(data), "region: "+bound,
				"metadata.region must equal the region bound into persisted state")
		})
	}
}

// TestEKSConfigReportsNameErrorAheadOfMissingRegion pins the precedence between the two create-time
// preconditions. Both are api.ErrInvalid, so without this the region check could silently take over
// every rejection in TestEKSConfigRejectsNonSegmentName and make that guard vacuous whenever the
// environment happens to carry no region.
func TestEKSConfigReportsNameErrorAheadOfMissingRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "")

	// "." is the discriminating case: unlike "foo/bar" it survives the state store's own name
	// validation, so the segment check added ahead of the region check is what must reject it.
	_, _, err := clusterapi.ExportEKSConfigForCreate(".")
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Contains(t, err.Error(), "single path segment")
	assert.NotContains(t, err.Error(), "no AWS region is selected")
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

// TestEKSConfigResolutionAfterCreateBindsToCreationRegion pins the resolution half of #6203: once a
// cluster exists, resolving its EKS config yields the region that created it rather than the one
// selected now. TestDeleteEKSReachesProvisionerBoundToCreationRegion covers the lifecycle half —
// that a successful action actually runs against that resolved region.
func TestEKSConfigResolutionAfterCreateBindsToCreationRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-north-1")

	const clusterName = "bound-eks"

	createPath, createRegion, err := clusterapi.ExportEKSConfigForCreate(clusterName)
	require.NoError(t, err)
	require.Equal(t, "eu-north-1", createRegion)

	// Persisted cluster state is what marks the create as complete, turning every later resolution
	// into a mutation of an existing remote cluster.
	require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
	}))

	// The operator now picks a different region in Settings.
	t.Setenv("AWS_REGION", "us-east-1")

	mutatePath, mutateRegion, err := clusterapi.ExportEKSConfigForCreate(clusterName)
	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", mutateRegion,
		"a post-create action must target the region the cluster was created in")
	assert.Equal(t, createPath, mutatePath)

	// The binding is also the only local evidence of the original target, so it must survive rather
	// than be rewritten with the newly selected region.
	data, err := os.ReadFile(createPath) //nolint:gosec // test-controlled path under a temp HOME.
	require.NoError(t, err)
	assert.Contains(t, string(data), "region: eu-north-1")
	assert.NotContains(t, string(data), "us-east-1")
}

// TestEKSConfigFollowsCurrentRegionUntilCreateCompletes pins the other side of the discriminator: a
// first create — and a retry after a failed one, which leaves no persisted state — must honour the
// region selected now, so a corrected region is not ignored.
func TestEKSConfigFollowsCurrentRegionUntilCreateCompletes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-north-1")

	const clusterName = "retried-eks"

	_, region, err := clusterapi.ExportEKSConfigForCreate(clusterName)
	require.NoError(t, err)
	require.Equal(t, "eu-north-1", region)

	t.Setenv("AWS_REGION", "us-east-1")

	_, retryRegion, err := clusterapi.ExportEKSConfigForCreate(clusterName)
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", retryRegion,
		"with no completed create, the region selected now must still apply")
}

// TestEKSConfigRefusesWhenBindingEvidenceIsMissing covers the fail-closed path: the cluster
// completed creation but its region evidence is gone, so the target cannot be confirmed. Falling
// back to the ambient region here is exactly the redirect this change prevents.
func TestEKSConfigRefusesWhenBindingEvidenceIsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-north-1")

	const clusterName = "evidence-gone"

	require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
	}))

	_, _, err := clusterapi.ExportEKSConfigForCreate(clusterName)
	require.ErrorIs(t, err, api.ErrInvalid)
}

// regionRecorder collects the region each named EKS factory build resolved, in order.
type regionRecorder struct {
	mu      sync.Mutex
	regions []string
}

func (r *regionRecorder) record(region string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.regions = append(r.regions, region)
}

// since returns the regions recorded at or after index n, so a test can assert about one phase
// without the earlier phase's resolutions bleeding into it.
func (r *regionRecorder) since(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.regions[min(n, len(r.regions)):])
}

func (r *regionRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.regions)
}

// newRegionRecordingEKSService wires a Service whose factory resolves the EKS distribution config
// exactly as the production defaultFactory does — from the cluster name alone — and records the
// region that resolution produced. That is what makes the region observable in a test: the region
// never travels in the action's Spec, it is read back from the on-disk eks.yaml when the provisioner
// is built.
func newRegionRecordingEKSService(
	t *testing.T,
	provisioner *fakeProvisioner,
	recorder *regionRecorder,
) *clusterapi.Service {
	t.Helper()

	// Only EKS gets the cluster-bearing provisioner. Handing the same one to every distribution
	// would let discovery find the cluster under Vanilla first, so the action would resolve as a
	// Vanilla cluster and never take the EKS path this test exists to exercise.
	empty := &fakeProvisioner{}

	return clusterapi.NewTestService(func(
		distribution v1alpha1.Distribution,
		name string,
	) (clusterprovisioner.Factory, error) {
		if distribution != v1alpha1.DistributionEKS {
			return fakeFactory{provisioner: empty}, nil
		}

		// Discovery builds a factory with no cluster name; only a named build resolves a config.
		if name != "" {
			_, region, err := clusterapi.ExportEKSConfigForCreate(name)
			if err != nil {
				return nil, fmt.Errorf("resolve EKS config for %q: %w", name, err)
			}

			recorder.record(region)
		}

		return fakeFactory{provisioner: provisioner}, nil
	})
}

// TestDeleteEKSReachesProvisionerBoundToCreationRegion is the lifecycle guard #6203 needs: a
// *successful* delete must reach the provisioner, and the config resolved to build that provisioner
// must carry the creation region even though a different one is selected now. The refusal tests
// below only prove that unconfirmed targets are blocked; without this one, a regression that bound
// the happy path to the current region would pass the whole suite.
func TestDeleteEKSReachesProvisionerBoundToCreationRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-north-1")

	const clusterName = "lifecycle-eks"

	recorder := &regionRecorder{}
	provisioner := &fakeProvisioner{}
	service := newRegionRecordingEKSService(t, provisioner, recorder)

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

	require.Equal(t, []string{"eu-north-1"}, recorder.since(0),
		"create must bind to the region selected at create time")

	// The operator selects a different region before deleting.
	beforeDelete := recorder.count()

	t.Setenv("AWS_REGION", "us-east-1")

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		return slices.Equal(provisioner.deletedNames(), []string{clusterName})
	}, eventuallyTimeout, eventuallyTick)

	deleteRegions := recorder.since(beforeDelete)
	require.NotEmpty(t, deleteRegions, "the delete must have built a provisioner for this cluster")

	for _, region := range deleteRegions {
		assert.Equal(t, "eu-north-1", region,
			"a successful delete must run against the creation region, not the one selected now")
	}
}

// TestDeleteEKSRefusesWithoutPersistedOwnershipState guards the destructive path directly: with no
// ownership state the backend cannot confirm which remote cluster it would delete, so it must
// refuse rather than proceed against an ambient target.
func TestDeleteEKSRefusesWithoutPersistedOwnershipState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "unbound-eks"

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

	require.NoError(t, state.DeleteClusterState(clusterName))

	err = service.Delete(context.Background(), "default", clusterName)
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Empty(t, provisioner.deletedNames(),
		"an unconfirmed target must never reach the provisioner")
}

// TestDeleteEKSRefusesWhenPersistedOwnershipStateDisagrees covers the inconsistent case: state that
// records a different backend than the cluster resolved as cannot describe this target.
func TestDeleteEKSRefusesWhenPersistedOwnershipStateDisagrees(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "mismatched-eks"

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
		Provider:     v1alpha1.ProviderDocker,
	}))

	err = service.Delete(context.Background(), "default", clusterName)
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Empty(t, provisioner.deletedNames())
}

// newReadyUnboundEKSService creates an EKS cluster, waits for it to settle, then removes the
// ownership state — the state an operator is in when KSail can no longer identify the remote target.
func newReadyUnboundEKSService(
	t *testing.T,
	clusterName string,
) (*clusterapi.Service, *fakeProvisioner) {
	t.Helper()

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

	require.NoError(t, state.DeleteClusterState(clusterName))

	return service, provisioner
}

// assertUnconfirmedEKSGuidance checks one refusal message: it must be an api.ErrInvalid, address the
// action the operator requested, point at recovery that can actually resolve this refusal, and offer
// deletion only on the delete path.
func assertUnconfirmedEKSGuidance(
	t *testing.T,
	err error,
	_ string,
	wantMessage string,
	wantDelete bool,
) {
	t.Helper()

	require.ErrorIs(t, err, api.ErrInvalid,
		"an unconfirmable EKS target must be refused on every mutating path")
	assert.Contains(t, err.Error(), wantMessage,
		"the recovery guidance must address the action the operator requested")
	assert.Contains(t, err.Error(), "eksctl",
		"every path must point at recovery that can actually resolve this refusal")
	assert.NotContains(t, err.Error(), "ksail cluster eks-bind",
		"eks-bind writes the immutable-identity record, not the create-time ownership state this "+
			"refusal reads, so offering it here loops the operator back to this same refusal")

	if !wantDelete {
		assert.NotContains(t, err.Error(), "eksctl delete cluster",
			"a non-destructive action must not be answered with a destructive recovery step")
	}
}

// TestUnconfirmedEKSMutationGuidanceMatchesTheRequestedAction pins the recovery guidance to the
// action the operator actually asked for. All three mutating entry points reach the same refusal —
// Delete as ClusterPhaseDeleting, Start and Stop as ClusterPhaseUpdating — so a single shared message
// told an operator who asked to start a cluster to delete it instead. Deleting is suggested only on
// the delete path.
//
// Every path must also point at recovery that can actually clear the refusal, and must NOT offer
// `ksail cluster eks-bind`. This assertion has been wrong twice in opposite directions, so it is
// worth stating precisely: an earlier round named a command the CLI does not expose, the next round
// claimed no such command existed, and the round after that named eks-bind — which does exist, but
// writes the region-scoped immutable-identity record (state.SaveEKSOwnershipState) rather than the
// create-time spec.json that state.LoadClusterSpec reads here. Naming an existing command is not the
// same claim as naming one that resolves this refusal; asserting only that the string is present
// pinned the second claim while testing the first.
func TestUnconfirmedEKSMutationGuidanceMatchesTheRequestedAction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, testCase := range []struct {
		name        string
		invoke      func(*clusterapi.Service, string) error
		wantDelete  bool
		wantMessage string
	}{
		{
			name: "delete",
			invoke: func(s *clusterapi.Service, cluster string) error {
				return s.Delete(context.Background(), "default", cluster)
			},
			wantDelete:  true,
			wantMessage: "eksctl delete cluster",
		},
		{
			name: "start",
			invoke: func(s *clusterapi.Service, cluster string) error {
				return s.Start(context.Background(), "default", cluster)
			},
			wantDelete:  false,
			wantMessage: "act on it with the AWS tooling directly",
		},
		{
			name: "stop",
			invoke: func(s *clusterapi.Service, cluster string) error {
				return s.Stop(context.Background(), "default", cluster)
			},
			wantDelete:  false,
			wantMessage: "act on it with the AWS tooling directly",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			clusterName := "unbound-eks-" + testCase.name
			service, provisioner := newReadyUnboundEKSService(t, clusterName)

			err := testCase.invoke(service, clusterName)

			assertUnconfirmedEKSGuidance(t, err, clusterName, testCase.wantMessage,
				testCase.wantDelete)
			assert.Empty(t, provisioner.deletedNames(),
				"a refused mutation must never reach the provisioner")
		})
	}
}

// TestEKSCreateFallsBackToTheScaffolderDefaultRegion is the regression Codex found: refusing the
// create when AWS_REGION is unset broke the supported default-region path, on a premise that does
// not hold. writeEKSConfig renders through scaffolder.DefaultEKSConfigParams, which substitutes its
// own default for an empty region — so an empty AWS_REGION was never going to reach metadata.region,
// and there was no region-less binding to protect against.
//
// What DOES matter for this PR is that the region bound into persisted state is the same one the
// file carries. Resolving the fallback at the call site is what guarantees that: both sides now read
// one value instead of deriving it independently.
func TestEKSCreateFallsBackToTheScaffolderDefaultRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "")

	const clusterName = "default-region-eks"

	configPath, region, err := clusterapi.ExportEKSConfigForCreate(clusterName)
	require.NoError(t, err, "an unset AWS_REGION must not refuse the create")

	expected := scaffolder.DefaultEKSConfigParams(clusterName, "").Region
	require.NotEmpty(t, expected, "the scaffolder default must be a real region")
	assert.Equal(t, expected, region, "the bound region must be the scaffolder default")

	//nolint:gosec // test-controlled path under a temp HOME.
	data, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "region: "+expected,
		"the written metadata.region must equal the region bound into persisted state")
}

// TestEKSCreateRejectsStateBelongingToAnotherDistribution covers the second finding on this PR:
// LoadClusterSpec persists state for EVERY distribution, so treating the mere existence of
// spec.json as proof that an EKS create completed misreads a Kind or Talos cluster of the same
// name. The bound path then looks for an eks.yaml that was never written, and the user sees a
// confusing read error instead of the name collision they actually have.
//
// A name collision is reported rather than silently treated as a fresh create: two clusters would
// otherwise share one state directory, and the second create would overwrite the first's record.
func TestEKSCreateRejectsStateBelongingToAnotherDistribution(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-central-1")

	const clusterName = "not-an-eks-cluster"

	require.NoError(t, state.SaveClusterSpec(clusterName, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionVanilla,
	}))

	_, _, err := clusterapi.ExportEKSConfigForCreate(clusterName)
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Contains(t, err.Error(), "Vanilla",
		"the error must name the distribution that actually owns the state")
	assert.NotContains(t, err.Error(), "read the eks config",
		"a name collision must not surface as a missing-eks.yaml read error")
}

// TestDeleteEKSClearsFailedCreateWithoutOwnershipState is the EKS analogue of
// TestDeleteClearsFailedClusterWithNoUnderlyingCluster: a create that failed before persisting
// ownership state must still be clearable. runCreate only writes spec.json once the provisioner
// succeeded, so a failed EKS create leaves the job Failed with no state — and because Create rejects
// any existing jobs entry, refusing the delete as well would strand the operator with a cluster they
// can neither clear nor retry until the server restarts.
//
// Clearing is local only: KSail never confirmed which remote cluster this was, so it must not aim a
// delete at AWS on the strength of a name.
func TestDeleteEKSClearsFailedCreateWithoutOwnershipState(t *testing.T) {
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

		phase, ok := phaseOf(list, clusterName)

		return ok && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	// Precondition: the failed create left no ownership state behind.
	_, loadErr := state.LoadClusterSpec(clusterName)
	require.ErrorIs(t, loadErr, state.ErrStateNotFound)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))

	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		_, present := phaseOf(list, clusterName)

		return !present
	}, eventuallyTimeout, eventuallyTick)

	// The remote cluster was never confirmed, so no AWS mutation may have been attempted.
	assert.Empty(t, provisioner.deletedNames())
}

// TestDeleteEKSRefusesLiveClusterWithoutOwnershipState is the negative control for the clearing path
// above: the exemption is scoped to a *failed create*, not to "ownership state is missing". A live
// EKS cluster whose state KSail cannot find is exactly the unconfirmed target the guard exists to
// protect, so it must still be refused.
func TestDeleteEKSRefusesLiveClusterWithoutOwnershipState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "live-eks"

	service, provisioner := newReadyUnboundEKSService(t, clusterName)

	err := service.Delete(context.Background(), "default", clusterName)
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Empty(t, provisioner.deletedNames())
}

// TestDeleteEKSRefusesFailedLifecycleJobWithoutOwnershipState is the second negative control, and it
// covers the case the first one cannot see. TestDeleteEKSRefusesLiveClusterWithoutOwnershipState has
// no tracked job at all, so it never reaches the clearing path; this one arrives there with a job in
// exactly the state that path inspects.
//
// Create, delete and start/stop all converge on Failed, so "failed EKS job with no ownership state"
// does not mean "failed create". A stop that fails leaves the remote cluster running — the stop is
// precisely the operation that did not take — and if its ownership state is then removed out of band,
// a clearing path keyed only on phase and distribution would drop the row and answer Delete with
// success while the AWS cluster is still there. That is worse than the refusal it replaced: the
// operator is told the cluster is gone.
//
// The distinguishing fact is the job's `origin`, which is fixed at registration and never changes.
func TestDeleteEKSRefusesFailedLifecycleJobWithoutOwnershipState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "stop-failed-eks"

	provisioner := &fakeProvisioner{stopErr: errSimulatedDeleteFailure}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: provisioner,
	})

	// A create that SUCCEEDS, so ownership state is persisted and the cluster is confirmed KSail's.
	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, clusterName)

		return ok && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)

	// A stop that fails. The job now sits in Failed — the same phase a failed create reaches, which
	// is what makes phase alone insufficient to tell them apart.
	require.NoError(t, service.Stop(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, clusterName)

		return ok && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	// The ownership state disappears underneath the failed job — removed out of band, or by a
	// delete run from the CLI while the server holds this job in memory. Only now do the two cases
	// look alike to a phase-and-distribution test.
	require.NoError(t, state.DeleteClusterState(clusterName))

	_, loadErr := state.LoadClusterSpec(clusterName)
	require.ErrorIs(t, loadErr, state.ErrStateNotFound)

	// Delete must still be refused: this was never a create, so KSail cannot claim no cluster was
	// left behind, and it must not aim a delete at AWS on the strength of a name either.
	err = service.Delete(context.Background(), "default", clusterName)
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Empty(t, provisioner.deletedNames(),
		"a refused delete must not have reached the provisioner")

	// The failed job must survive the refusal. Clearing it would destroy the only record of why the
	// stop did not take, which is the operator's route back to a working cluster.
	list, listErr := service.List(context.Background())
	require.NoError(t, listErr)

	phase, present := phaseOf(list, clusterName)
	assert.True(t, present, "the refused delete must not have cleared the failed job")
	assert.Equal(t, v1alpha1.ClusterPhaseFailed, phase,
		"the job must still report why the stop failed")
}

// TestDeleteEKSRefusesJobReplacedDuringOwnershipRead is the third negative control, and it covers a
// window the other two cannot reach. clearedFailedEKSCreate releases the lock to read ownership state
// from disk, so the entry it validated before that read is not necessarily the entry it removes after
// it: the job can be replaced in between — ownership state restored out of band, then a start/stop
// registered and failed.
//
// Re-checking only the phase on the way out is therefore not enough. Create, delete and start/stop all
// converge on Failed, so a replacement failed *stop* satisfies a phase-only re-check while describing
// a cluster that may still be running in AWS. That is precisely the confusion `origin` was introduced
// to prevent, and TestDeleteEKSRefusesFailedLifecycleJobWithoutOwnershipState pins it for the
// steady-state case; this pins it across the unlocked window, where the check has to be repeated in
// full rather than assumed to still hold.
//
// The replacement is driven through the ownership-read seam rather than a competing goroutine, so the
// interleaving is exact and the test cannot flake.
func TestDeleteEKSRefusesJobReplacedDuringOwnershipRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "replaced-during-read"

	provisioner := &fakeProvisioner{createErr: errSimulatedCreateFailure}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: provisioner,
	})

	// A create that fails, so the job really is a failed create: the pre-read check passes and the
	// clearing path proceeds into the unlocked ownership read.
	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionEKS),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, clusterName)

		return ok && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	// Act inside the unlocked window: replace the failed create with a failed stop, then answer the
	// read exactly as the real loader would for a cluster with no persisted state.
	reads := 0

	service.SetLoadClusterSpecForTest(func(name string) (*v1alpha1.ClusterSpec, error) {
		reads++

		service.ReplaceJobWithFailedStopForTest(name)

		return nil, state.ErrStateNotFound
	})

	err = service.Delete(context.Background(), "default", clusterName)

	// Without the read actually happening the test would pass vacuously — it would be asserting the
	// steady-state refusal, not the windowed one.
	require.Equal(t, 1, reads,
		"the unlocked ownership read must have run, or the window under test was never entered")
	require.ErrorIs(t, err, api.ErrInvalid,
		"a job that became a failed stop during the read must not be cleared as a failed create")
	assert.Empty(t, provisioner.deletedNames(),
		"a refused delete must not have reached the provisioner")
	assert.True(t, service.JobPresentForTest(clusterName),
		"the replacement failed stop must survive: it is the only record of a cluster"+
			" that may still be running")
}

// TestDeleteFailedLocalCreateStillReachesTheProvisioner pins the distribution scope of the
// failed-create clearing path. Clearing the job locally is right for EKS, where the ownership guard
// stands between KSail and a remote account it cannot identify. It would be wrong for a local
// distribution: a half-finished Kind or vCluster create leaves containers behind, and the
// provisioner's Delete is what removes them. Local distributions must therefore still go through the
// provisioner rather than having their job quietly dropped.
func TestDeleteFailedLocalCreateStillReachesTheProvisioner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "half-made"

	provisioner := &fakeProvisioner{createErr: errSimulatedCreateFailure}
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionVanilla: provisioner,
	})

	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionVanilla),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, ok := phaseOf(list, clusterName)

		return ok && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))

	require.Eventually(t, func() bool {
		return slices.Contains(provisioner.deletedNames(), clusterName)
	}, eventuallyTimeout, eventuallyTick)
}

// TestDeleteEKSRefusesADifferentFailedCreateInTheSameWindow is the sharper half of the windowed race,
// and the one a full re-check of the predicate does NOT catch. A replacement that is itself a failed
// EKS create matches phase, origin and distribution exactly, so every field-based test passes while
// the entry is a different operation's failure.
//
// Clearing it would report success for a delete that never inspected this create, and silently
// discard the record of why the second create failed. Only the job's identity separates "the entry I
// approved" from "an entry just like it".
func TestDeleteEKSRefusesADifferentFailedCreateInTheSameWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "replaced-by-a-twin"

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

		phase, ok := phaseOf(list, clusterName)

		return ok && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	reads := 0

	service.SetLoadClusterSpecForTest(func(name string) (*v1alpha1.ClusterSpec, error) {
		reads++

		service.ReplaceJobWithAnotherFailedEKSCreateForTest(name)

		return nil, state.ErrStateNotFound
	})

	err = service.Delete(context.Background(), "default", clusterName)

	require.Equal(t, 1, reads,
		"the unlocked ownership read must have run, or the window under test was never entered")
	require.ErrorIs(t, err, api.ErrInvalid,
		"a delete must not clear a failed create it never approved, however alike the two look")
	assert.True(t, service.JobPresentForTest(clusterName),
		"the replacement create's failure must survive: clearing it discards the only record of it")
}

// ownershipRecordFor is the immutable identity AWS confirmed at create time, which is what binds a
// cluster to a region once its create completed.
func ownershipRecordFor(name, region string) *state.EKSOwnershipState {
	return &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: name,
		Region:      region,
		AccountID:   "123456789012",
		ClusterARN:  "arn:aws:eks:" + region + ":123456789012:cluster/" + name,
		CreatedAt:   time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC),
		AWSOptions: v1alpha1.OptionsAWS{ //nolint:gosec // G101: env-var NAMES, never credential values.
			ProfileEnvVar:         "AWS_PROFILE",
			RegionEnvVar:          "AWS_REGION",
			AccessKeyIDEnvVar:     "AWS_ACCESS_KEY_ID",
			SecretAccessKeyEnvVar: "AWS_SECRET_ACCESS_KEY",
			SessionTokenEnvVar:    "AWS_SESSION_TOKEN",
		},
	}
}

// TestBoundEKSConfigBindsFromOwnershipWhenTheStateConfigIsAbsent covers clusters created the NORMAL
// way. `ksail cluster create` writes persisted cluster state (which marks the create complete) but
// scaffolds eks.yaml into the PROJECT directory, while only the local API backend writes one under
// ~/.ksail/clusters/<name>. The bound path therefore entered on state it trusted and then failed
// reading a file nothing had put there — breaking every start, stop and delete for exactly the
// clusters created the ordinary way. The ownership record is the authoritative binding for both.
func TestBoundEKSConfigBindsFromOwnershipWhenTheStateConfigIsAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// A region that is NOT the recorded one: binding must come from the record, never the ambient
	// environment, or this test would pass while the redirect it guards was still possible.
	t.Setenv("AWS_REGION", "us-west-2")

	const name = "cli-created"

	require.NoError(t, state.SaveClusterSpec(name, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
	}))
	require.NoError(
		t,
		state.SaveEKSOwnershipState(name, "eu-north-1", ownershipRecordFor(name, "eu-north-1")),
	)

	configPath, region, err := clusterapi.ExportEKSConfigForCreate(name)
	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", region, "the creation region must come from the ownership record")
	assert.FileExists(t, configPath, "the provisioner still needs the config on disk")

	// Assert the region inside the FILE, not just the returned one. eksctl reads the file, so a
	// config written from the ambient region while the returned value carried the bound one would
	// aim the action at us-west-2 and still satisfy every assertion above.
	data, err := os.ReadFile(configPath) //nolint:gosec // test-controlled path under a temp HOME.
	require.NoError(t, err)
	assert.Contains(t, string(data), "region: eu-north-1",
		"the generated eks.yaml must carry the bound region, not the ambient one")
	assert.NotContains(t, string(data), "us-west-2",
		"the ambient region must not reach the file the provisioner reads")
}

// TestBoundEKSConfigRefusesAmbiguousOwnership is the destructive-path guard on that binding. Two
// ownership records mean two same-named clusters in different AWS regions, and nothing on disk says
// which one an operator meant. Choosing either would aim a delete at a cluster nobody named, so the
// only safe answer is to refuse and say what it found.
func TestBoundEKSConfigRefusesAmbiguousOwnership(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_REGION", "eu-north-1")

	const name = "two-regions"

	require.NoError(t, state.SaveClusterSpec(name, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
		Provider:     v1alpha1.ProviderAWS,
	}))

	for _, region := range []string{"eu-north-1", "us-east-1"} {
		require.NoError(
			t,
			state.SaveEKSOwnershipState(name, region, ownershipRecordFor(name, region)),
		)
	}

	_, _, err := clusterapi.ExportEKSConfigForCreate(name)
	require.ErrorIs(t, err, api.ErrInvalid)
	assert.Contains(t, err.Error(), "more than one region")
	assert.Contains(t, err.Error(), "eu-north-1")
	assert.Contains(t, err.Error(), "us-east-1")
}

// TestDeleteResolvesAPersistedEKSTargetOutsideTheSelectedRegion covers the other half of the same
// binding. Live enumeration lists only the region selected NOW, so an EKS cluster created in another
// region — or one the UI has simply been repointed away from since, which is reproducible by
// restarting it with a different AWS_REGION — vanished from the listing, and start/stop/delete
// reported "not found" before the ownership check could run. The cluster is not missing; the ambient
// region is just pointed elsewhere, and the persisted record says so.
//
// The assertion is deliberately about which ANSWER is reached, not about the mutation succeeding:
// resolving must hand the request to confirmEKSOwnership rather than short-circuit it, so a target
// KSail cannot account for is still refused — just never as an absence.
func TestDeleteResolvesAPersistedEKSTargetOutsideTheSelectedRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "other-region"

	// No provisioner lists this cluster: discovery in the currently-selected region cannot see it.
	service := newTestService(map[v1alpha1.Distribution]*fakeProvisioner{
		v1alpha1.DistributionEKS: {},
	})

	// The control: with no record either, "not found" is the correct and expected answer.
	require.ErrorIs(t,
		service.Delete(context.Background(), "default", clusterName),
		api.ErrNotFound,
		"a cluster with no live listing and no ownership record really is unknown")

	require.NoError(t, state.SaveEKSOwnershipState(
		clusterName, "ap-southeast-2", ownershipRecordFor(clusterName, "ap-southeast-2"),
	))

	err := service.Delete(context.Background(), "default", clusterName)
	require.NotErrorIs(t, err, api.ErrNotFound,
		"a cluster KSail holds an ownership record for is not missing; the selected region is")
}

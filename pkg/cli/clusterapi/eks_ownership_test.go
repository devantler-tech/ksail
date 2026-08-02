package clusterapi_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errGuardRefused stands in for any reason the ownership guard fails closed (a missing immutable
// record, an account mismatch, an unreachable AWS identity endpoint).
var errGuardRefused = errors.New("ownership guard refused")

// guardRecorder captures whether the factory the service built for an action carried an EKS
// mutation guard, so a test can assert the wiring without reaching AWS.
type guardRecorder struct {
	mu       sync.Mutex
	guarded  bool
	verifier eksidentity.Verifier
}

func (r *guardRecorder) record(verifier eksidentity.Verifier) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.guarded = true
	r.verifier = verifier
}

func (r *guardRecorder) wasGuarded() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.guarded
}

func (r *guardRecorder) recordedVerifier() eksidentity.Verifier {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.verifier
}

// guardObservingFactory is a fake factory that also satisfies the optional guard-carrying interface, so
// the assertion is about what the service handed the factory rather than about AWS behaviour.
type guardObservingFactory struct {
	provisioner *fakeProvisioner
	recorder    *guardRecorder
}

func (f guardObservingFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return f.provisioner, nil, nil
}

func (f guardObservingFactory) WithEKSMutationGuard(
	_ *credentials.AWSResolution,
	verifier eksidentity.Verifier,
) clusterprovisioner.Factory {
	f.recorder.record(verifier)

	return f
}

// newGuardRecordingService wires a service whose EKS factory records the guard it receives and
// whose guard resolution is stubbed, so no test touches AWS.
func newGuardRecordingService(
	t *testing.T,
	observed v1alpha1.Distribution,
	provisioner *fakeProvisioner,
	recorder *guardRecorder,
	guardErr error,
) *clusterapi.Service {
	t.Helper()

	// Route only the distribution under test to the observed provisioner. Discovery is restricted
	// to the Docker provider, so handing it the same provisioner would make it enumerate the
	// cluster as a Docker one; a lifecycle action would then resolve the distribution as Vanilla,
	// the guard would correctly decline to fire, and the assertion would be measuring the harness
	// rather than the product.
	empty := &fakeProvisioner{}
	service := clusterapi.NewTestService(func(
		distribution v1alpha1.Distribution,
		_ string,
	) (clusterprovisioner.Factory, error) {
		if distribution != observed {
			return fakeFactory{provisioner: empty}, nil
		}

		return guardObservingFactory{provisioner: provisioner, recorder: recorder}, nil
	})
	service.SetEKSOwnershipGuardForTest(
		func(
			_ context.Context,
			_ string,
		) (credentials.AWSResolution, eksidentity.Verifier, error) {
			if guardErr != nil {
				return credentials.AWSResolution{}, nil, guardErr
			}

			return credentials.AWSResolution{}, func(context.Context) error { return nil }, nil
		},
	)

	return service
}

// TestDeleteEKSCarriesOwnershipVerifierIntoTheFactory pins the wiring this issue is about: a local
// API EKS delete must hand the provisioner the immutable-ownership verifier, so the provisioner
// re-checks identity at its own mutation boundary exactly as the standalone CLI path does. Without
// it the factory is built with a nil verifier and VerifyBeforeMutation is a no-op — a destructive
// action with no ownership query at all.
func TestDeleteEKSCarriesOwnershipVerifierIntoTheFactory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "guarded-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionEKS, provisioner, recorder, nil)

	createEKSClusterForTest(t, service, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		return len(provisioner.deletedNames()) == 1
	}, eventuallyTimeout, eventuallyTick)

	assert.True(t, recorder.wasGuarded(),
		"EKS delete built its factory without an ownership guard")
	assert.NotNil(t, recorder.recordedVerifier(),
		"EKS delete carried a nil ownership verifier, which VerifyBeforeMutation skips")
}

// TestDeleteEKSRefusesWhenOwnershipCannotBeVerified pins the fail-closed half: when the immutable
// identity cannot be established the destructive action must not reach the provisioner at all.
func TestDeleteEKSRefusesWhenOwnershipCannotBeVerified(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "unverifiable-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(
		t, v1alpha1.DistributionEKS, provisioner, recorder, errGuardRefused,
	)

	createEKSClusterForTest(t, service, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	assert.Empty(t, provisioner.deletedNames(),
		"an unverifiable EKS target was deleted anyway")
}

// TestDeleteEKSRefusesWhenOwnershipResolutionReturnsNoVerifier pins the fail-open this guard is
// most likely to acquire by accident: resolution succeeding but yielding no verifier. Downstream a
// nil verifier reads as "nothing to check", so carrying it would produce a mutation that looks
// guarded at every layer and checks nothing.
func TestDeleteEKSRefusesWhenOwnershipResolutionReturnsNoVerifier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "verifierless-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionEKS, provisioner, recorder, nil)

	createEKSClusterForTest(t, service, clusterName)

	service.SetEKSOwnershipGuardForTest(
		func(
			_ context.Context,
			_ string,
		) (credentials.AWSResolution, eksidentity.Verifier, error) {
			return credentials.AWSResolution{}, nil, nil
		},
	)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	assert.Empty(t, provisioner.deletedNames(),
		"a nil ownership verifier was carried into the mutation instead of refusing it")
}

// TestStopEKSRefusesWhenOwnershipCannotBeVerified covers the nodegroup-scaling boundary: stop scales
// managed nodegroups to zero, so it is a mutation and carries the same guard as delete.
func TestStopEKSRefusesWhenOwnershipCannotBeVerified(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "unverifiable-stop-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(
		t, v1alpha1.DistributionEKS, provisioner, recorder, errGuardRefused,
	)

	createEKSClusterForTest(t, service, clusterName)

	require.NoError(t, service.Stop(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	assert.Empty(t, provisioner.stoppedNames(),
		"an unverifiable EKS target was stopped anyway")
}

// TestCreateEKSDoesNotRequireOwnershipVerification is the over-tightening control. A create has no
// prior incarnation to verify and no persisted identity yet, so demanding a guard there would make
// every first EKS create from the web UI fail. It must still succeed with the guard refusing.
func TestCreateEKSDoesNotRequireOwnershipVerification(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "fresh-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(
		t, v1alpha1.DistributionEKS, provisioner, recorder, errGuardRefused,
	)

	createEKSClusterForTest(t, service, clusterName)

	assert.False(t, recorder.wasGuarded(),
		"an EKS create resolved an ownership guard, which no first create can satisfy")
}

// TestDeleteNonEKSDoesNotResolveAnOwnershipGuard is the scope control: the guard is an AWS identity
// concern, so a Docker-backed distribution must be untouched by it.
func TestDeleteNonEKSDoesNotResolveAnOwnershipGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "vanilla-cluster"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(
		t, v1alpha1.DistributionVanilla, provisioner, recorder, errGuardRefused,
	)

	_, err := service.Create(
		context.Background(),
		clusterFor(clusterName, v1alpha1.DistributionVanilla),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		return len(provisioner.deletedNames()) == 1
	}, eventuallyTimeout, eventuallyTick)

	assert.False(t, recorder.wasGuarded(),
		"a non-EKS delete resolved an EKS ownership guard")
}

// createEKSClusterForTest drives a cluster through the async create path to Ready, which is the
// precondition every lifecycle assertion above starts from.
func createEKSClusterForTest(t *testing.T, service *clusterapi.Service, name string) {
	t.Helper()

	_, err := service.Create(context.Background(), clusterFor(name, v1alpha1.DistributionEKS))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, name)

		return found && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)
}

package clusterapi_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
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

// TestEKSOwnershipVerificationIsBounded pins the deadline on the guard's network calls.
//
// The mutation paths run on a context.WithoutCancel background context, so the HTTP request that
// triggered the action can never cancel this work, and the AWS SDK applies no overall per-operation
// deadline of its own. Without an explicit bound, an unresponsive STS or EKS endpoint leaves the job
// pinned in Deleting with no way to dismiss it — the undismissable-row failure runDelete's
// idempotency handling already exists to avoid. A hung guard must therefore fail the job, not hang.
func TestEKSOwnershipVerificationIsBounded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "hung-endpoint-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionEKS, provisioner, recorder, nil)

	createEKSClusterForTest(t, service, clusterName)

	// A guard that never answers, exactly as an unresponsive AWS endpoint behaves.
	sawDeadline := make(chan bool, 1)

	service.SetEKSOwnershipTimeoutForTest(50 * time.Millisecond)
	service.SetEKSOwnershipGuardForTest(
		func(
			ctx context.Context,
			_ string,
		) (credentials.AWSResolution, eksidentity.Verifier, error) {
			_, hasDeadline := ctx.Deadline()
			select {
			case sawDeadline <- hasDeadline:
			default:
			}

			<-ctx.Done()

			return credentials.AWSResolution{}, nil, ctx.Err()
		},
	)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick)

	assert.True(t, <-sawDeadline,
		"the ownership guard ran without a deadline, so a hung AWS endpoint would pin the job")
	assert.Empty(t, provisioner.deletedNames(),
		"a delete proceeded despite ownership verification never completing")
}

// TestCarriedVerifierIsBoundedPerInvocation pins the deadline on the verifier handed to the
// provisioner, not only on this package's own first check.
//
// The provisioner re-checks identity at its own mutation boundary using the context IT was given —
// which is the same uncancellable background context. Handing the raw verifier onward would leave
// that second check unbounded, so a hung STS or EKS endpoint could still pin the job even though the
// first check returned quickly. The guarantee has to hold at both boundaries.
func TestCarriedVerifierIsBoundedPerInvocation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "carried-verifier-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionEKS, provisioner, recorder, nil)

	createEKSClusterForTest(t, service, clusterName)

	service.SetEKSOwnershipTimeoutForTest(50 * time.Millisecond)
	// This is the exact scenario the finding describes: the FIRST check succeeds quickly, so a
	// guard is built and carried onward, and only the later mutation-boundary check hangs. A
	// verifier that hung immediately would fail the first check and never reach the factory at all.
	var calls atomic.Int32

	service.SetEKSOwnershipGuardForTest(
		func(
			_ context.Context,
			_ string,
		) (credentials.AWSResolution, eksidentity.Verifier, error) {
			return credentials.AWSResolution{}, func(ctx context.Context) error {
				if calls.Add(1) == 1 {
					return nil
				}

				<-ctx.Done()

				return ctx.Err()
			}, nil
		},
	)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		return recorder.recordedVerifier() != nil
	}, eventuallyTimeout, eventuallyTick)

	carried := recorder.recordedVerifier()
	require.NotNil(t, carried)

	// Invoke it exactly as the provisioner does: with the uncancellable background context.
	done := make(chan error, 1)

	go func() { done <- carried(context.WithoutCancel(context.Background())) }()

	select {
	case err := <-done:
		require.Error(t, err, "a hung verifier returned success")
	case <-time.After(2 * time.Second):
		t.Fatal("the carried verifier ran unbounded — a hung AWS endpoint would pin the job here")
	}
}

// TestCreateEKSCapturesOwnershipIdentity pins that a successful create records the identity the
// guard later verifies against. Without it the guard blocks the very clusters this API creates.
func TestCreateEKSCapturesOwnershipIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "captured-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionEKS, provisioner, recorder, nil)

	captured := make(chan string, 1)

	service.SetEKSOwnershipCaptureForTest(func(_ context.Context, name string) error {
		captured <- name

		return nil
	})

	createEKSClusterForTest(t, service, clusterName)

	select {
	case got := <-captured:
		assert.Equal(t, clusterName, got)
	default:
		t.Fatal("a successful EKS create recorded no ownership identity, so every later " +
			"delete/start/stop would fail with the rebind error")
	}
}

// TestCreateEKSFailsWhenOwnershipCannotBeCaptured is the fail-loud half: the remote cluster exists
// either way, so reporting success while leaving it unoperatable would hide the problem until the
// first delete. This mirrors how a SaveClusterSpec failure is already handled.
func TestCreateEKSFailsWhenOwnershipCannotBeCaptured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "uncapturable-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionEKS, provisioner, recorder, nil)

	service.SetEKSOwnershipCaptureForTest(func(_ context.Context, _ string) error {
		return errGuardRefused
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
}

// TestNonEKSCreateCapturesNothing is the scope control: capture is an AWS identity concern.
func TestNonEKSCreateCapturesNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingService(t, v1alpha1.DistributionVanilla, provisioner, recorder, nil)

	service.SetEKSOwnershipCaptureForTest(func(_ context.Context, _ string) error {
		t.Error("a non-EKS create attempted an AWS ownership capture")

		return nil
	})

	_, err := service.Create(
		context.Background(),
		clusterFor("plain-cluster", v1alpha1.DistributionVanilla),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, "plain-cluster")

		return found && phase == v1alpha1.ClusterPhaseReady
	}, eventuallyTimeout, eventuallyTick)
}

// newGuardRecordingServiceWithVerifierErr wires a service whose ownership resolution succeeds but
// whose verifier fails. Resolution and verification fail for different reasons — resolution fails
// when the evidence cannot be read, verification when the live cluster contradicts it — so a test
// about verification outcomes has to drive the verifier itself.
func newGuardRecordingServiceWithVerifierErr(
	t *testing.T,
	provisioner *fakeProvisioner,
	recorder *guardRecorder,
	verifyErr error,
) *clusterapi.Service {
	t.Helper()

	empty := &fakeProvisioner{}
	service := clusterapi.NewTestService(func(
		distribution v1alpha1.Distribution,
		_ string,
	) (clusterprovisioner.Factory, error) {
		if distribution != v1alpha1.DistributionEKS {
			return fakeFactory{provisioner: empty}, nil
		}

		return guardObservingFactory{provisioner: provisioner, recorder: recorder}, nil
	})
	service.SetEKSOwnershipGuardForTest(
		func(
			_ context.Context,
			_ string,
		) (credentials.AWSResolution, eksidentity.Verifier, error) {
			return credentials.AWSResolution{}, func(context.Context) error {
				return verifyErr
			}, nil
		},
	)

	return service
}

// TestDeleteEKSStaysIdempotentWhenTheClusterIsAlreadyGone pins the distinction the guard has to
// draw: a cluster that is absent is not a cluster that is wrong.
//
// Verification cannot succeed against a cluster that no longer exists, so a guard that treats every
// verification failure alike turns the ordinary out-of-band-deletion case into a permanent Failed
// row plus retained completed-create state, which then blocks recreating the same name. That is the
// undismissable-row trap runDelete's idempotency handling exists to avoid — reintroduced one layer
// above it.
func TestDeleteEKSStaysIdempotentWhenTheClusterIsAlreadyGone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "vanished-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingServiceWithVerifierErr(
		t, provisioner, recorder,
		// The shape production produces: eksidentity wraps whatever DescribeCluster returned.
		fmt.Errorf("describe EKS cluster for ownership verification: %w",
			fmt.Errorf("%w: %s", eksclient.ErrClusterNotFound, clusterName)),
	)

	createEKSClusterForTest(t, service, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		_, found := phaseOf(list, clusterName)

		return !found
	}, eventuallyTimeout, eventuallyTick,
		"an EKS delete for an already-absent cluster stayed pinned instead of clearing")

	assert.Empty(t, provisioner.deletedNames(),
		"a delete was issued against a cluster the guard had established was gone")
}

// TestDeleteEKSStillRefusesAnIdentityMismatch is the other direction, and the reason the test above
// cannot simply relax the guard: an identity mismatch means a DIFFERENT live cluster answers to this
// name, so the mutation must still be refused. Absence and mismatch must not collapse into one case.
func TestDeleteEKSStillRefusesAnIdentityMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "mismatched-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newGuardRecordingServiceWithVerifierErr(
		t, provisioner, recorder,
		fmt.Errorf("%w: live cluster ARN does not match persisted ARN",
			eksidentity.ErrIdentityMismatch),
	)

	createEKSClusterForTest(t, service, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick,
		"an identity mismatch did not fail closed")

	assert.Empty(t, provisioner.deletedNames(),
		"a delete reached the provisioner despite an ownership identity mismatch")
}

// carriedVerifierFactory hands the recorded verifier to a provisioner that actually CALLS it, so a
// test can exercise the second verification boundary. The real EKS provisioner re-checks ownership
// at its own mutation boundary via VerifyBeforeMutation and returns whatever that yields; the fakes
// used elsewhere in this file only record the verifier, so they cannot reach this path at all.
type carriedVerifierFactory struct {
	provisioner *fakeProvisioner
	recorder    *guardRecorder
}

func (f carriedVerifierFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return &carriedVerifierProvisioner{
		inner:    f.provisioner,
		verifier: f.recorder.recordedVerifier(),
	}, nil, nil
}

func (f carriedVerifierFactory) WithEKSMutationGuard(
	_ *credentials.AWSResolution,
	verifier eksidentity.Verifier,
) clusterprovisioner.Factory {
	f.recorder.record(verifier)

	return f
}

// carriedVerifierProvisioner re-checks ownership before delegating, exactly as the real provisioner
// does at its mutation boundary.
type carriedVerifierProvisioner struct {
	inner    *fakeProvisioner
	verifier eksidentity.Verifier
}

func (p *carriedVerifierProvisioner) Create(ctx context.Context, name string) error {
	return p.inner.Create(ctx, name)
}

func (p *carriedVerifierProvisioner) Delete(ctx context.Context, name string) error {
	if p.verifier != nil {
		err := p.verifier(ctx)
		if err != nil {
			return fmt.Errorf("verify before mutation: %w", err)
		}
	}

	return p.inner.Delete(ctx, name)
}

func (p *carriedVerifierProvisioner) Start(ctx context.Context, name string) error {
	return p.inner.Start(ctx, name)
}

func (p *carriedVerifierProvisioner) Stop(ctx context.Context, name string) error {
	return p.inner.Stop(ctx, name)
}

func (p *carriedVerifierProvisioner) List(ctx context.Context) ([]string, error) {
	return p.inner.List(ctx)
}

func (p *carriedVerifierProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	return p.inner.Exists(ctx, name)
}

// newCarriedVerifierService wires a service whose provisioner calls the CARRIED verifier, with a
// verifier that succeeds on its first call (the guard's own check) and fails on the second (the
// provisioner's mutation-boundary re-check).
func newCarriedVerifierService(
	t *testing.T,
	provisioner *fakeProvisioner,
	recorder *guardRecorder,
	secondCallErr error,
) *clusterapi.Service {
	t.Helper()

	empty := &fakeProvisioner{}
	service := clusterapi.NewTestService(func(
		distribution v1alpha1.Distribution,
		_ string,
	) (clusterprovisioner.Factory, error) {
		if distribution != v1alpha1.DistributionEKS {
			return fakeFactory{provisioner: empty}, nil
		}

		return carriedVerifierFactory{provisioner: provisioner, recorder: recorder}, nil
	})

	var calls atomic.Int32

	service.SetEKSOwnershipGuardForTest(
		func(
			_ context.Context,
			_ string,
		) (credentials.AWSResolution, eksidentity.Verifier, error) {
			return credentials.AWSResolution{}, func(context.Context) error {
				if calls.Add(1) == 1 {
					return nil
				}

				return secondCallErr
			}, nil
		},
	)

	return service
}

// TestDeleteEKSStaysIdempotentWhenTheClusterVanishesBeforeTheCarriedCheck covers the window the
// carried verifier exists for: the cluster is present when the guard verifies and gone by the time
// the provisioner re-checks at its own mutation boundary.
//
// Normalizing absence only at the guard's own call would leave this second boundary returning a raw
// eksclient error that runDelete does not recognize — the same undismissable Failed row and retained
// state, reached one boundary later.
func TestDeleteEKSStaysIdempotentWhenTheClusterVanishesBeforeTheCarriedCheck(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "vanished-late-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newCarriedVerifierService(t, provisioner, recorder,
		fmt.Errorf("describe EKS cluster for ownership verification: %w",
			fmt.Errorf("%w: %s", eksclient.ErrClusterNotFound, clusterName)))

	createEKSClusterForTest(t, service, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		_, found := phaseOf(list, clusterName)

		return !found
	}, eventuallyTimeout, eventuallyTick,
		"a cluster that vanished before the carried verification stayed pinned Failed")
}

// TestDeleteEKSStillRefusesALateIdentityMismatch is the other direction at the same boundary: a
// mismatch discovered by the carried verifier must still fail closed.
func TestDeleteEKSStillRefusesALateIdentityMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const clusterName = "mismatched-late-eks"

	provisioner := &fakeProvisioner{}
	recorder := &guardRecorder{}
	service := newCarriedVerifierService(t, provisioner, recorder,
		fmt.Errorf("%w: live cluster ARN does not match persisted ARN",
			eksidentity.ErrIdentityMismatch))

	createEKSClusterForTest(t, service, clusterName)

	require.NoError(t, service.Delete(context.Background(), "default", clusterName))
	require.Eventually(t, func() bool {
		list, listErr := service.List(context.Background())
		require.NoError(t, listErr)

		phase, found := phaseOf(list, clusterName)

		return found && phase == v1alpha1.ClusterPhaseFailed
	}, eventuallyTimeout, eventuallyTick,
		"a late identity mismatch did not fail closed")

	assert.Empty(t, provisioner.deletedNames(),
		"a delete was delegated despite the carried verifier reporting a mismatch")
}

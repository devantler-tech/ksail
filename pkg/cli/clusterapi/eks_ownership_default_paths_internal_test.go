package clusterapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/require"
)

// The tests in this file cover defaultEKSCapture and defaultEKSGuard — the capture and guard
// closures the production factory is actually wired to (local_service.go), as opposed to the
// injected seams every other test in this package drives.
//
// The distinction is not academic. Deleting the identity refusal from defaultEKSCapture outright
// left the whole ownership suite green: the seams around it are covered at 95-100%, and these two
// functions at 0.0%, so a security refusal could be removed from the shipped path without a single
// test noticing. That is the same shape of gap this PR exists to close, one level down.
//
// Every branch asserted here returns BEFORE any AWS call, which is what makes the shipped path
// testable without credentials at all.

// eksOwnershipService builds a Service with only what these paths read.
func eksOwnershipService() *Service {
	return &Service{eksOwnershipTimeout: time.Second}
}

// markEKSCreated makes eksCreateCompleted report true, which is what lets boundEKSConfig proceed
// past its "not created yet" short circuit to the binding lookup.
func markEKSCreated(t *testing.T, name string) {
	t.Helper()

	require.NoError(t, state.SaveClusterSpec(name, &v1alpha1.ClusterSpec{
		Distribution: v1alpha1.DistributionEKS,
	}))
}

// TestCaptureRefusesWhenTheCreateWroteNoBinding covers the first refusal in the shipped capture
// path. A capture with no binding has no region to record the identity against, and inventing one
// from the ambient selection is the divergence the pinned identity exists to remove.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestCaptureRefusesWhenTheCreateWroteNoBinding(t *testing.T) {
	isolateHome(t)

	err := eksOwnershipService().
		defaultEKSCapture(context.Background(), "unbound", &eksCreateIdentity{
			awsOptions: recordedAliases(),
		})

	require.Error(t, err)
	require.ErrorIs(t, err, ErrEKSOwnershipEvidenceMissing)
}

// TestCaptureRefusesWhenTheCreatePinnedNoIdentity is the branch whose deletion the suite did not
// notice. Reaching it requires a real binding, so the refusal cannot be reached by accident from an
// empty home — the earlier bound==nil check would answer first and the test would pass for the
// wrong reason.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestCaptureRefusesWhenTheCreatePinnedNoIdentity(t *testing.T) {
	isolateHome(t)

	const (
		name   = "bound-but-unpinned"
		region = "eu-west-1"
	)

	markEKSCreated(t, name)
	saveOwnership(t, name, region, recordedAliases())

	err := eksOwnershipService().defaultEKSCapture(context.Background(), name, nil)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrEKSOwnershipEvidenceMissing)
	require.Contains(t, err.Error(), "pinned no AWS identity")
}

func TestDefaultEKSCaptureRefusesRepointedSelector(t *testing.T) {
	isolateHome(t)

	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_PROFILE", "original")

	identity := newEKSCreateIdentity(v1alpha1.OptionsAWS{})

	name := "clusterapi-test"
	markEKSCreated(t, name)
	saveOwnership(t, name, "us-east-1", recordedAliases())

	t.Setenv("AWS_PROFILE", "repointed")

	err := eksOwnershipService().defaultEKSCapture(t.Context(), name, identity)
	if !errors.Is(err, ErrEKSOwnershipSelectorChanged) {
		t.Fatalf("expected ErrEKSOwnershipSelectorChanged, got %v", err)
	}
}

func TestDefaultEKSCaptureDoesNotRefuseMatchingSelector(t *testing.T) {
	isolateHome(t)

	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_PROFILE", "original")

	identity := newEKSCreateIdentity(v1alpha1.OptionsAWS{})

	name := "clusterapi-test"
	markEKSCreated(t, name)
	saveOwnership(t, name, "us-east-1", recordedAliases())

	err := eksOwnershipService().defaultEKSCapture(t.Context(), name, identity)
	if errors.Is(err, ErrEKSOwnershipSelectorChanged) {
		t.Fatalf("matching selector must not be refused as a selector change, got %v", err)
	}
}

// TestCaptureReachesTheIdentityRefusalOnlyThroughARealBinding is the control that makes the test
// above attributable. With the binding removed and everything else identical, the SAME call must
// still fail — but on the earlier branch, with the other message. Without this, an implementation
// that refused every capture unconditionally would satisfy the assertion above.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestCaptureReachesTheIdentityRefusalOnlyThroughARealBinding(t *testing.T) {
	isolateHome(t)

	err := eksOwnershipService().defaultEKSCapture(context.Background(), "no-binding", nil)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrEKSOwnershipEvidenceMissing)
	require.NotContains(t, err.Error(), "pinned no AWS identity")
	require.Contains(t, err.Error(), "wrote no target binding")
}

// TestGuardRefusesWhenNoBindingWasEverWritten covers the shipped guard's refusal. A mutation must
// resolve its target from the binding written at create time; resolving from the region selected
// now is precisely the redirect the guard exists to catch, so the absence of a binding can never
// fall back to the ambient one.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestGuardRefusesWhenNoBindingWasEverWritten(t *testing.T) {
	isolateHome(t)

	resolution, verifier, err := eksOwnershipService().
		defaultEKSGuard(context.Background(), "unbound")

	require.Error(t, err)
	require.ErrorIs(t, err, ErrEKSOwnershipEvidenceMissing)
	require.Nil(t, verifier)
	require.Equal(t, credentials.AWSResolution{}, resolution)
	require.Contains(t, err.Error(), "eks-bind")
}

// TestGuardAndCaptureAgreeOnTheMissingEvidenceSentinel keeps the two shipped paths reporting the
// same class of failure. They are read by one caller that decides whether a mutation may proceed;
// a sentinel drift would make one of them fail open there while both still returned an error.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestGuardAndCaptureAgreeOnTheMissingEvidenceSentinel(t *testing.T) {
	isolateHome(t)

	_, _, guardErr := eksOwnershipService().defaultEKSGuard(context.Background(), "unbound")
	captureErr := eksOwnershipService().
		defaultEKSCapture(context.Background(), "unbound", &eksCreateIdentity{})

	require.ErrorIs(t, guardErr, ErrEKSOwnershipEvidenceMissing)
	require.ErrorIs(t, captureErr, ErrEKSOwnershipEvidenceMissing)
}

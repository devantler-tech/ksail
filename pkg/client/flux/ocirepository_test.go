package flux_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newFakeOCIRepositoryReadiness builds the root flux-system OCIRepository with a
// Ready=True condition and an optional status.lastHandledReconcileAt — the shape
// the reconcile-request gate in WaitForOCIRepositoryReady inspects. Ready is
// always True so the tests isolate the token gate; condition evaluation itself is
// covered by TestEvaluateOCIRepositoryConditions.
func newFakeOCIRepositoryReadiness(lastHandledReconcileAt string) *unstructured.Unstructured {
	repo := &unstructured.Unstructured{}
	repo.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "source.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "OCIRepository",
	})
	repo.SetName(ociRepoName)
	repo.SetNamespace(namespaceFluxSystem)

	status := map[string]any{
		statusConditions: []any{
			map[string]any{"type": conditionTypeReady, "status": statusTrue},
		},
	}
	if lastHandledReconcileAt != "" {
		status["lastHandledReconcileAt"] = lastHandledReconcileAt
	}

	repo.Object["status"] = status

	return repo
}

// newFakeReconcilingOCIRepository builds the root OCIRepository with Ready=True
// but also a Reconciling=True condition and a handled reconcile token — the shape
// the source controller reports mid-reconcile, when it has acknowledged our
// request but not yet served the new artifact (so Ready still reflects the prior
// revision). WaitForOCIRepositoryReady must NOT trust this Ready (bug #5717).
func newFakeReconcilingOCIRepository(lastHandledReconcileAt string) *unstructured.Unstructured {
	repo := newFakeOCIRepositoryReadiness(lastHandledReconcileAt)

	status, _ := repo.Object["status"].(map[string]any)
	status[statusConditions] = []any{
		map[string]any{"type": conditionTypeReady, "status": statusTrue},
		map[string]any{"type": conditionTypeReconciling, "status": statusTrue},
	}

	return repo
}

func TestReconcileRequestHandled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		lastHandled   string
		expectedToken string
		want          bool
	}{
		{name: "empty token is always handled", lastHandled: "old", expectedToken: "", want: true},
		{
			name:          "matching token is handled",
			lastHandled:   "tok-1",
			expectedToken: "tok-1",
			want:          true,
		},
		{
			name:          "stale token is not handled",
			lastHandled:   "tok-0",
			expectedToken: "tok-1",
			want:          false,
		},
		{
			name:          "absent status with token is not handled",
			lastHandled:   "",
			expectedToken: "tok-1",
			want:          false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			repo := newFakeOCIRepositoryReadiness(testCase.lastHandled)

			got := flux.ReconcileRequestHandled(repo, testCase.expectedToken)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestWaitForOCIRepositoryReadyGatesOnHandledToken(t *testing.T) {
	t.Parallel()

	t.Run("matching handled token with Ready returns immediately", func(t *testing.T) {
		t.Parallel()

		reconcilerClient := newTestFluxReconcilerWithSources(
			newFakeOCIRepositoryReadiness("tok-1"),
		)

		err := reconcilerClient.WaitForOCIRepositoryReady(
			context.Background(),
			time.Second,
			"tok-1",
		)
		require.NoError(t, err)
	})

	t.Run("empty token preserves condition-only readiness", func(t *testing.T) {
		t.Parallel()

		reconcilerClient := newTestFluxReconcilerWithSources(
			newFakeOCIRepositoryReadiness(""),
		)

		err := reconcilerClient.WaitForOCIRepositoryReady(context.Background(), time.Second, "")
		require.NoError(t, err)
	})

	t.Run(
		"stale Ready with unhandled token times out (does not accept pre-push state)",
		func(t *testing.T) {
			t.Parallel()

			// Ready=True but the source has only handled an OLD reconcile request, so
			// the current Ready reflects a pre-push revision. The wait must NOT accept
			// it — it times out instead of racing the ingest (bug #5717).
			reconcilerClient := newTestFluxReconcilerWithSources(
				newFakeOCIRepositoryReadiness("tok-0"),
			)

			err := reconcilerClient.WaitForOCIRepositoryReady(
				context.Background(), 200*time.Millisecond, "tok-1",
			)
			require.Error(t, err)
		},
	)

	t.Run(
		"handled token still reconciling times out (does not trust mid-reconcile Ready)",
		func(t *testing.T) {
			t.Parallel()

			// The controller has acknowledged our request (token matches) but is still
			// reconciling, so Ready=True reflects the prior artifact. The wait must keep
			// waiting rather than trusting the stale Ready (bug #5717).
			reconcilerClient := newTestFluxReconcilerWithSources(
				newFakeReconcilingOCIRepository("tok-1"),
			)

			err := reconcilerClient.WaitForOCIRepositoryReady(
				context.Background(), 200*time.Millisecond, "tok-1",
			)
			require.Error(t, err)
		},
	)
}

func TestWaitForOCIRepositoryReadyWaitsForReconcileToSettle(t *testing.T) {
	t.Parallel()

	// The source has handled our request but is still reconciling on the first
	// poll (Ready is the prior revision's), then settles (Reconciling cleared) —
	// the wait must block through the reconciling poll and succeed only once the
	// object settles (bug #5717).
	reconciling := newFakeReconcilingOCIRepository("tok-1")
	settled := newFakeOCIRepositoryReadiness("tok-1")

	reconcilerClient := newTestFluxReconcilerWithSources(reconciling)

	var polls atomic.Int32

	fakeClient, ok := reconcilerClient.Dynamic.(*dynamicfake.FakeDynamicClient)
	require.True(t, ok, "expected a fake dynamic client")

	fakeClient.PrependReactor(
		"get", "ocirepositories",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			if polls.Add(1) <= 1 {
				return true, reconciling, nil
			}

			return true, settled, nil
		},
	)

	err := reconcilerClient.WaitForOCIRepositoryReady(context.Background(), 5*time.Second, "tok-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(
		t, polls.Load(), int32(2), "should have polled through the reconciling status",
	)
}

func TestReconcileInProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		conditions []reconciler.Condition
		want       bool
	}{
		{name: "no conditions is not reconciling", conditions: nil, want: false},
		{
			name:       "reconciling true is in progress",
			conditions: []reconciler.Condition{{Type: "Reconciling", Status: "True"}},
			want:       true,
		},
		{
			name:       "reconciling false is settled",
			conditions: []reconciler.Condition{{Type: "Reconciling", Status: "False"}},
			want:       false,
		},
		{
			name:       "only ready present is settled",
			conditions: []reconciler.Condition{{Type: "Ready", Status: "True"}},
			want:       false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := flux.ReconcileInProgress(testCase.conditions)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestWaitForOCIRepositoryReadyWaitsForHandledThenReady(t *testing.T) {
	t.Parallel()

	// The source serves the stale (old-token) status for the first poll, then the
	// controller handles our request (token matches, Ready) — the wait must block
	// through the stale poll and succeed once the request is handled.
	stale := newFakeOCIRepositoryReadiness("tok-0")
	fresh := newFakeOCIRepositoryReadiness("tok-1")

	reconcilerClient := newTestFluxReconcilerWithSources(stale)

	var polls atomic.Int32

	fakeClient, ok := reconcilerClient.Dynamic.(*dynamicfake.FakeDynamicClient)
	require.True(t, ok, "expected a fake dynamic client")

	fakeClient.PrependReactor(
		"get", "ocirepositories",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			if polls.Add(1) <= 1 {
				return true, stale, nil
			}

			return true, fresh, nil
		},
	)

	err := reconcilerClient.WaitForOCIRepositoryReady(context.Background(), 5*time.Second, "tok-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, polls.Load(), int32(2), "should have polled through the stale status")
}

// Static sentinel errors for the table-driven cases (err113 forbids inline
// errors.New in tests). isPermanentOCIError classifies on the rendered message,
// so these sentinels exercise the substring paths.
var (
	errOCIManifestUnknown = errors.New("GET https://ghcr.io/...: manifest unknown")
	errOCIDoesNotExist    = errors.New("artifact does not exist in the registry")
	errOCIGeneric         = errors.New("dial tcp: connection refused")
	errOCILastObserved    = errors.New("last observed transient error")
)

func newOCINotFoundError() error {
	return apierrors.NewNotFound(
		schema.GroupResource{Group: "source.toolkit.fluxcd.io", Resource: "ocirepositories"},
		"flux-system",
	)
}

func TestIsPermanentOCIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error is not permanent", err: nil, want: false},
		{name: "kubernetes NotFound is transient", err: newOCINotFoundError(), want: false},
		{name: "manifest unknown is permanent", err: errOCIManifestUnknown, want: true},
		{name: "does not exist is permanent", err: errOCIDoesNotExist, want: true},
		{name: "generic error is not permanent", err: errOCIGeneric, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := flux.IsPermanentOCIError(testCase.err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

//nolint:funlen // Table-driven test with comprehensive cases
func TestEvaluateOCIRepositoryConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		conditions []reconciler.Condition
		wantReady  bool
		wantErr    bool
	}{
		{
			name:       "no conditions is still progressing",
			conditions: nil,
			wantReady:  false,
			wantErr:    false,
		},
		{
			name: "ready true returns ready",
			conditions: []reconciler.Condition{
				{Type: "Ready", Status: "True"},
			},
			wantReady: true,
			wantErr:   false,
		},
		{
			name: "OCIPullFailed is a permanent failure",
			conditions: []reconciler.Condition{
				{
					Type:    "Ready",
					Status:  "False",
					Reason:  "OCIPullFailed",
					Message: "manifest unknown",
				},
			},
			wantReady: false,
			wantErr:   true,
		},
		{
			name: "OCIArtifactPullFailed is a permanent failure",
			conditions: []reconciler.Condition{
				{
					Type:    "Ready",
					Status:  "False",
					Reason:  "OCIArtifactPullFailed",
					Message: "not found",
				},
			},
			wantReady: false,
			wantErr:   true,
		},
		{
			name: "ready false with other reason keeps waiting",
			conditions: []reconciler.Condition{
				{Type: "Ready", Status: "False", Reason: "Progressing"},
			},
			wantReady: false,
			wantErr:   false,
		},
		{
			name: "non-ready condition type is skipped",
			conditions: []reconciler.Condition{
				{Type: "Stalled", Status: "True", Reason: "SomeReason"},
			},
			wantReady: false,
			wantErr:   false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ready, err := flux.EvaluateOCIRepositoryConditions(testCase.conditions)

			assert.Equal(t, testCase.wantReady, ready)

			if testCase.wantErr {
				require.ErrorIs(t, err, flux.ErrOCIRepositoryNotReady)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestOCITimeoutError(t *testing.T) {
	t.Parallel()

	t.Run("nil last error falls back to ErrOCIRepositoryNotReady", func(t *testing.T) {
		t.Parallel()

		err := flux.OCITimeoutError(nil)
		require.ErrorIs(t, err, flux.ErrOCIRepositoryNotReady)
	})

	t.Run("non-nil last error is returned verbatim", func(t *testing.T) {
		t.Parallel()

		err := flux.OCITimeoutError(errOCILastObserved)
		require.ErrorIs(t, err, errOCILastObserved)
	})
}

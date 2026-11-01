package flux_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type requestReadinessCase struct {
	name, handled, ready, reason, status, progress string
	reconciling, suspended, wantReady, wantFailure bool
}

// TestKustomizationRequestReadiness separates old conditions from a fresh result.
func TestKustomizationRequestReadiness(t *testing.T) {
	t.Parallel()

	tests := []requestReadinessCase{
		{
			name: "old failure remains pending", handled: "old",
			ready: "False", reason: "ReconciliationFailed", status: "handle",
		},
		{name: "old success remains pending", handled: "old", ready: "True", status: "handle"},
		{name: "current success completes", handled: "requested", ready: "True", wantReady: true},
		{
			name: "current failure fails", handled: "requested", ready: "False",
			reason: "ReconciliationFailed", wantFailure: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			checkKustomizationRequestCase(t, testCase)
		})
	}
}

// TestKustomizationRequestCompletionBoundaries covers Flux's retry and suspension states.
func TestKustomizationRequestCompletionBoundaries(t *testing.T) {
	t.Parallel()

	tests := []requestReadinessCase{
		{
			name: "finished failure awaiting retry fails promptly", handled: "requested",
			ready: "False", reason: "ReconciliationFailed", reconciling: true,
			progress: "ProgressingWithRetry", wantFailure: true,
		},
		{
			name: "acknowledged work is unfinished", handled: "requested",
			ready: "True", reconciling: true, status: "in progress",
		},
		{
			name:        "inconsistent retry success remains pending",
			handled:     "requested",
			ready:       "True",
			reconciling: true,
			progress:    "ProgressingWithRetry",
			status:      "in progress",
		},
		{
			name: "suspension does not wait for acknowledgement", ready: "False",
			reason: "ReconciliationFailed", suspended: true, wantReady: true, status: "suspended",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			checkKustomizationRequestCase(t, testCase)
		})
	}
}

func checkKustomizationRequestCase(t *testing.T, testCase requestReadinessCase) {
	t.Helper()

	resource := newFakeKustomizationWithSource(
		"apps", "OCIRepository", testCase.ready, testCase.reason, "fixture result",
		pushedRevision, pushedRevision,
	)
	object := resource.Object
	require.NoError(
		t,
		unstructured.SetNestedField(object, testCase.handled, "status", "lastHandledReconcileAt"),
	)
	require.NoError(t, unstructured.SetNestedField(object, testCase.suspended, "spec", "suspend"))

	if testCase.reconciling {
		conditions, _, err := unstructured.NestedSlice(object, "status", "conditions")
		require.NoError(t, err)

		conditions = append(conditions, map[string]any{
			"type": "Reconciling", "status": "True", "reason": testCase.progress,
		})
		require.NoError(t, unstructured.SetNestedSlice(object, conditions, "status", "conditions"))
	}

	client := newTestFluxReconcilerWithSources(
		resource,
		newFakeSource("OCIRepository", pushedRevision),
	)

	ready, status, err := client.CheckNamedKustomizationReadyForRequest(
		t.Context(),
		"apps",
		"requested",
	)
	if testCase.wantFailure {
		require.ErrorIs(t, err, flux.ErrKustomizationFailed)
	} else {
		require.NoError(t, err)
	}

	assert.Equal(t, testCase.wantReady, ready)

	if testCase.status != "" {
		assert.Contains(t, status, testCase.status)
	}
}

// TestKustomizationRequestRequiresToken refuses an accidental unbound success.
func TestKustomizationRequestRequiresToken(t *testing.T) {
	t.Parallel()

	resource := newFakeKustomizationWithSource("apps", "OCIRepository", "True", "", "", "", "")
	client := newTestFluxReconcilerWithSources(resource)
	ready, _, err := client.CheckNamedKustomizationReadyForRequest(t.Context(), "apps", "")
	require.ErrorContains(t, err, "token is required")
	assert.False(t, ready)
}

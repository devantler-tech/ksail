package flux_test

import (
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

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

package workload_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newArgoCDPollClient serves scripted API responses to the real polling loop.
func newArgoCDPollClient(
	reactor k8stesting.ReactionFunc,
) *argocd.Reconciler {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	client.PrependReactor("get", "applications", reactor)

	return &argocd.Reconciler{Base: reconciler.NewBaseWithClient(client)}
}

// argoCDPollApplication retains healthy status alongside an optional active comparison error.
func argoCDPollApplication(message string) *unstructured.Unstructured {
	conditions := []any{}
	if message != "" {
		conditions = append(conditions, map[string]any{
			"type": "ComparisonError", "message": message,
		})
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata":   map[string]any{"name": "test-app", "namespace": "argocd"},
		"status": map[string]any{
			"sync":       map[string]any{"status": "Synced"},
			"health":     map[string]any{"status": "Healthy"},
			"conditions": conditions,
		},
	}}
}

// TestPollArgoCDApplicationRecoversFromTransportFailure waits for an error-free comparison.
func TestPollArgoCDApplicationRecoversFromTransportFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	client := newArgoCDPollClient(func(action k8stesting.Action) (bool, runtime.Object, error) {
		assert.Equal(t, "argocd", action.GetNamespace())
		getAction, ok := action.(k8stesting.GetAction)
		require.True(t, ok)
		assert.Equal(t, "test-app", getAction.GetName())

		if calls.Add(1) == 1 {
			return true, argoCDPollApplication("lookup registry.example: no such host"), nil
		}

		return true, argoCDPollApplication(""), nil
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	err := workload.ExportPollUntilApplicationReady(ctx, client, "test-app")
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load(), "stale healthy status must not finish the first poll")
}

// TestPollArgoCDApplicationFailsFastOnMissingArtifact rejects permanent failures on the first poll.
func TestPollArgoCDApplicationFailsFastOnMissingArtifact(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	client := newArgoCDPollClient(func(_ k8stesting.Action) (bool, runtime.Object, error) {
		calls.Add(1)

		return true, argoCDPollApplication("manifest unknown"), nil
	})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err := workload.ExportPollUntilApplicationReady(ctx, client, "test-app")
	require.ErrorIs(t, err, argocd.ErrSourceNotAvailable)
	require.NotErrorIs(t, err, argocd.ErrReconcileTimeout)
	assert.Contains(t, err.Error(), "test-app")
	assert.Equal(t, int32(1), calls.Load())
}

// TestPollArgoCDApplicationTimeoutPreservesTransportCause keeps failure details and an inspection command.
func TestPollArgoCDApplicationTimeoutPreservesTransportCause(t *testing.T) {
	t.Parallel()

	client := newArgoCDPollClient(func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, argoCDPollApplication("connection refused"), nil
	})

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	err := workload.ExportPollUntilApplicationReady(ctx, client, "test-app")
	require.ErrorIs(t, err, argocd.ErrReconcileTimeout)
	assert.Contains(t, err.Error(), "test-app")
	assert.Contains(t, err.Error(), "connection refused")
	assert.Contains(
		t,
		err.Error(),
		"ksail workload get applications.argoproj.io test-app -n argocd",
	)
}

// TestPollArgoCDApplicationCancellation preserves cancellation during a transient failure.
func TestPollArgoCDApplicationCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := newArgoCDPollClient(func(_ k8stesting.Action) (bool, runtime.Object, error) {
		cancel()

		return true, argoCDPollApplication("connection refused"), nil
	})

	err := workload.ExportPollUntilApplicationReady(ctx, client, "test-app")
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, argocd.ErrReconcileTimeout)
}

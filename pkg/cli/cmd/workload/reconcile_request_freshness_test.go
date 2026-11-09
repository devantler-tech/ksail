package workload_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestKustomizationTaskWaitsForFreshReconcileResult reproduces a failed same-revision
// attempt that remains visible until the controller handles this task's own request.
func TestKustomizationTaskWaitsForFreshReconcileResult(t *testing.T) {
	t.Parallel()

	const revision = "same-revision"

	resource := staleRequestKustomization(revision)
	source := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "OCIRepository",
		"metadata": map[string]any{"name": "flux-system", "namespace": flux.DefaultNamespace},
		"status":   map[string]any{"artifact": map[string]any{"revision": revision}},
	}}
	api := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), resource, source)

	var (
		requested    atomic.Pointer[string]
		afterRequest atomic.Int32
		servedFresh  atomic.Bool
	)

	api.PrependReactor(
		"patch",
		"kustomizations",
		captureReconcileRequest(t, &requested, &afterRequest),
	)
	api.PrependReactor("get", "kustomizations", freshResultAfterSecondRead(
		resource, revision, &requested, &afterRequest, &servedFresh,
	))
	client := &flux.Reconciler{Base: reconciler.NewBaseWithClient(api)}
	tasks := workload.ExportBuildKustomizationTasks(
		[]flux.KustomizationInfo{
			{Name: "apps"},
		},
		nil,
		client,
		&workload.ExportFailedKustomizations{},
	)
	require.Len(t, tasks, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, tasks[0].Fn(ctx), "a stale failed result must not terminate a new request")

	token := requested.Load()
	require.NotNil(t, token)
	require.NotEmpty(t, *token, "each task must request its own reconciliation")
	require.True(t, servedFresh.Load(), "success needs the controller's fresh matching receipt")
}

// freshResultAfterSecondRead leaves one stale read after each new request.
func freshResultAfterSecondRead(
	resource *unstructured.Unstructured,
	revision string,
	requested *atomic.Pointer[string],
	afterRequest *atomic.Int32,
	servedFresh *atomic.Bool,
) k8stesting.ReactionFunc {
	return func(_ k8stesting.Action) (bool, runtime.Object, error) {
		result := resource.DeepCopy()

		token := requested.Load()
		if token != nil {
			result.SetAnnotations(map[string]string{"reconcile.fluxcd.io/requestedAt": *token})
		}

		if token != nil && *token != "" && afterRequest.Add(1) >= 2 {
			result.Object["status"] = map[string]any{
				"observedGeneration": int64(1), "lastAttemptedRevision": revision,
				"lastAppliedRevision": revision, "lastHandledReconcileAt": *token,
				"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
			}

			servedFresh.Store(true)
		}

		return true, result, nil
	}
}

// captureReconcileRequest lets the fake API tracker apply the real merge patch.
func captureReconcileRequest(
	t *testing.T,
	requested *atomic.Pointer[string],
	afterRequest *atomic.Int32,
) k8stesting.ReactionFunc {
	t.Helper()

	return func(action k8stesting.Action) (bool, runtime.Object, error) {
		patch, ok := action.(k8stesting.PatchAction)
		require.True(t, ok)

		var payload struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}

		err := json.Unmarshal(patch.GetPatch(), &payload)
		if err != nil {
			return true, nil, fmt.Errorf("decode requested annotation: %w", err)
		}

		token := payload.Metadata.Annotations["reconcile.fluxcd.io/requestedAt"]

		afterRequest.Store(0)
		requested.Store(&token)

		return false, nil, nil
	}
}

// staleRequestKustomization represents a completed failure at the current source revision.
func staleRequestKustomization(revision string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1", "kind": "Kustomization",
		"metadata": map[string]any{
			"name": "apps", "namespace": flux.DefaultNamespace, "generation": int64(1),
		},
		"spec": map[string]any{
			"sourceRef": map[string]any{"kind": "OCIRepository", "name": "flux-system"},
		},
		"status": map[string]any{
			"observedGeneration": int64(1), "lastAttemptedRevision": revision,
			"lastHandledReconcileAt": "previous-request",
			"conditions": []any{map[string]any{
				"type": "Ready", "status": "False", "reason": "ReconciliationFailed",
				"message": "the previous attempt failed",
			}},
		},
	}}
}

// TestKustomizationTaskUnacknowledgedRequestKeepsDeadline never promotes old failure
// to a current permanent verdict and preserves the caller's original deadline.
func TestKustomizationTaskUnacknowledgedRequestKeepsDeadline(t *testing.T) {
	t.Parallel()

	api := dynamicfake.NewSimpleDynamicClient(
		runtime.NewScheme(),
		staleRequestKustomization("same-revision"),
	)
	client := &flux.Reconciler{Base: reconciler.NewBaseWithClient(api)}
	tasks := workload.ExportBuildKustomizationTasks(
		[]flux.KustomizationInfo{
			{Name: "apps"},
		},
		nil,
		client,
		&workload.ExportFailedKustomizations{},
	)
	require.Len(t, tasks, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()

	err := tasks[0].Fn(ctx)
	require.ErrorIs(t, err, flux.ErrReconcileTimeout)
	require.NotErrorIs(t, err, flux.ErrKustomizationFailed)
}

// TestKustomizationTaskBlockedDependencyDoesNotRequest stops before any API mutation.
func TestKustomizationTaskBlockedDependencyDoesNotRequest(t *testing.T) {
	t.Parallel()

	failed := &workload.ExportFailedKustomizations{}
	workload.ExportRecordKustomizationFailure(
		failed,
		"infra",
		workload.ExportErrDependencyBlocked(),
	)
	tasks := workload.ExportBuildKustomizationTasks(
		[]flux.KustomizationInfo{{Name: "apps", DependsOn: []string{"infra"}}}, nil, nil, failed,
	)
	require.Len(t, tasks, 1)
	require.ErrorIs(t, tasks[0].Fn(t.Context()), workload.ExportErrDependencyBlocked())
}

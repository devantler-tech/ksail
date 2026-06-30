package flux_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

const (
	oldRevision    = "latest@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	pushedRevision = "latest@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	ociRepoName    = "flux-system"
)

// ociRepositoryGVR / gitRepositoryGVR are the source GVRs registered with the
// fake dynamic client for revision-aware readiness tests.
var (
	ociRepositoryGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test-scoped constant
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "ocirepositories",
	}
	gitRepositoryGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test-scoped constant
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "gitrepositories",
	}
)

// newTestFluxReconcilerWithSources creates a Flux Reconciler backed by a fake
// dynamic client that supports Kustomization, OCIRepository, and GitRepository
// resources — needed to exercise revision-aware readiness.
func newTestFluxReconcilerWithSources(objects ...runtime.Object) *flux.Reconciler {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			kustomizationGVR: kustomizationListKind,
			ociRepositoryGVR: "OCIRepositoryList",
			gitRepositoryGVR: "GitRepositoryList",
		},
		objects...,
	)

	return &flux.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}
}

// newFakeKustomizationWithSource builds a Kustomization CR with a spec.sourceRef
// (always the root flux-system source) and status.lastAttemptedRevision /
// lastAppliedRevision, alongside the usual Ready condition — the shape needed
// for revision-aware readiness checks.
func newFakeKustomizationWithSource(
	name, sourceKind string,
	readyStatus, readyReason, readyMessage string,
	lastAttempted, lastApplied string,
) *unstructured.Unstructured {
	kust := &unstructured.Unstructured{}
	kust.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kustomize.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    kustomizationKind,
	})
	kust.SetName(name)
	kust.SetNamespace(namespaceFluxSystem)

	kust.Object["spec"] = map[string]any{
		"path": "./" + name,
		"sourceRef": map[string]any{
			"kind": sourceKind,
			"name": ociRepoName,
		},
	}

	status := map[string]any{
		statusConditions: []any{
			map[string]any{
				"type":    conditionTypeReady,
				"status":  readyStatus,
				"reason":  readyReason,
				"message": readyMessage,
			},
		},
	}
	if lastAttempted != "" {
		status["lastAttemptedRevision"] = lastAttempted
	}

	if lastApplied != "" {
		status["lastAppliedRevision"] = lastApplied
	}

	kust.Object["status"] = status

	return kust
}

// newFakeSource builds an OCIRepository/GitRepository CR (the root flux-system
// source) carrying a status.artifact.revision.
func newFakeSource(kind, revision string) *unstructured.Unstructured {
	src := &unstructured.Unstructured{}
	src.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "source.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    kind,
	})
	src.SetName(ociRepoName)
	src.SetNamespace(namespaceFluxSystem)

	if revision != "" {
		src.Object["status"] = map[string]any{
			"artifact": map[string]any{"revision": revision},
		}
	}

	return src
}

//nolint:funlen // Table-driven test with comprehensive revision-aware cases.
func TestCheckNamedKustomizationReadyRevisionAware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		objects     []runtime.Object
		wantReady   bool
		wantStatus  string
		wantErr     bool
		wantErrType error
	}{
		{
			// The platform deadlock: a leftover BuildFailed from the OLD revision
			// must NOT fail the gate when the just-pushed revision hasn't been
			// attempted yet — keep polling so the fix can heal the layer.
			name: "stale BuildFailed on old revision keeps polling (no false-fail)",
			objects: []runtime.Object{
				newFakeSource("OCIRepository", pushedRevision),
				newFakeKustomizationWithSource(
					statusApps, "OCIRepository",
					statusFalse, "BuildFailed", "envsubst error on old revision",
					oldRevision, "",
				),
			},
			wantReady:  false,
			wantStatus: "waiting for revision",
		},
		{
			// A leftover Ready=True from the OLD revision must NOT pass the gate
			// before the new (possibly broken) revision is applied.
			name: "stale Ready=True on old revision keeps polling (no false-pass)",
			objects: []runtime.Object{
				newFakeSource("OCIRepository", pushedRevision),
				newFakeKustomizationWithSource(
					statusApps, "OCIRepository",
					statusTrue, reasonSucceeded, "Applied revision: old",
					oldRevision, oldRevision,
				),
			},
			wantReady:  false,
			wantStatus: "waiting for revision",
		},
		{
			name: "ready on current revision passes",
			objects: []runtime.Object{
				newFakeSource("OCIRepository", pushedRevision),
				newFakeKustomizationWithSource(
					statusApps, "OCIRepository",
					statusTrue, reasonSucceeded, "Applied revision: pushed",
					pushedRevision, pushedRevision,
				),
			},
			wantReady:  true,
			wantStatus: conditionTypeReady,
		},
		{
			// A real BuildFailed of the CURRENT revision is correctly attributed
			// and fails the gate.
			name: "BuildFailed on current revision fails",
			objects: []runtime.Object{
				newFakeSource("OCIRepository", pushedRevision),
				newFakeKustomizationWithSource(
					statusApps, "OCIRepository",
					statusFalse, "BuildFailed", "envsubst error on pushed revision",
					pushedRevision, oldRevision,
				),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
		},
		{
			// Ready=True but applied revision still trails the source → the apply
			// is in flight, keep polling.
			name: "ready but applied revision trails source keeps polling",
			objects: []runtime.Object{
				newFakeSource("OCIRepository", pushedRevision),
				newFakeKustomizationWithSource(
					statusApps, "OCIRepository",
					statusTrue, reasonSucceeded, "Applied revision: old",
					pushedRevision, oldRevision,
				),
			},
			wantReady:  false,
			wantStatus: "waiting for revision",
		},
		{
			// A never-attempted kustomization (empty lastAttemptedRevision)
			// renders the source revision and a "<none>" attempted revision.
			name: "never-attempted kustomization keeps polling",
			objects: []runtime.Object{
				newFakeSource("OCIRepository", pushedRevision),
				newFakeKustomizationWithSource(
					statusApps, "OCIRepository",
					statusFalse, "Progressing", "reconciliation in progress",
					"", "",
				),
			},
			wantReady:  false,
			wantStatus: "<none>",
		},
		{
			// Short (non-digest) revisions are rendered verbatim.
			name: "short revision ready passes",
			objects: []runtime.Object{
				newFakeSource("GitRepository", "v1"),
				newFakeKustomizationWithSource(
					statusInfra, "GitRepository",
					statusTrue, reasonSucceeded, "Applied revision: v1",
					"v1", "v1",
				),
			},
			wantReady:  true,
			wantStatus: conditionTypeReady,
		},
		{
			// GitRepository sources are resolved the same way.
			name: "git source ready on current revision passes",
			objects: []runtime.Object{
				newFakeSource("GitRepository", pushedRevision),
				newFakeKustomizationWithSource(
					statusInfra, "GitRepository",
					statusTrue, reasonSucceeded, "Applied revision: pushed",
					pushedRevision, pushedRevision,
				),
			},
			wantReady:  true,
			wantStatus: conditionTypeReady,
		},
		{
			// Backward-compat: when the source has no artifact revision yet, fall
			// back to condition-only readiness (Ready=True → ready).
			name: "source without artifact falls back to condition-only ready",
			objects: []runtime.Object{
				newFakeSource("OCIRepository", ""),
				newFakeKustomizationWithSource(
					statusApps, "OCIRepository",
					statusTrue, reasonSucceeded, "Applied revision: x",
					"", "",
				),
			},
			wantReady:  true,
			wantStatus: conditionTypeReady,
		},
		{
			// Backward-compat: an unknown source kind cannot be resolved → fall
			// back to condition-only readiness.
			name: "unknown source kind falls back to condition-only ready",
			objects: []runtime.Object{
				newFakeKustomizationWithSource(
					statusApps, "HelmRepository",
					statusTrue, reasonSucceeded, "Applied revision: x",
					"", "",
				),
			},
			wantReady:  true,
			wantStatus: conditionTypeReady,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := newTestFluxReconcilerWithSources(testCase.objects...)

			ready, status, err := r.CheckNamedKustomizationReady(
				context.Background(),
				kustomizationNameForTest(testCase.objects),
			)

			if testCase.wantErr {
				require.Error(t, err)

				if testCase.wantErrType != nil {
					require.ErrorIs(t, err, testCase.wantErrType)
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantReady, ready)
			assert.Contains(t, status, testCase.wantStatus)
		})
	}
}

// kustomizationNameForTest returns the name of the first Kustomization object in
// the fixture set, so each case checks the kustomization it defined.
func kustomizationNameForTest(objects []runtime.Object) string {
	for _, obj := range objects {
		u, ok := obj.(*unstructured.Unstructured)
		if ok && u.GetKind() == kustomizationKind {
			return u.GetName()
		}
	}

	return ""
}

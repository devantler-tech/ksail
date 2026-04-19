package flux_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/client/flux"
	"github.com/devantler-tech/ksail/v6/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var errSimulatedAPIFailure = errors.New("simulated API failure")

// kustomizationGVR is the GroupVersionResource for Flux Kustomization CRs.
var kustomizationGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test-scoped constant
	Group:    "kustomize.toolkit.fluxcd.io",
	Version:  "v1",
	Resource: "kustomizations",
}

// newTestFluxReconciler creates a Flux Reconciler backed by a fake dynamic client
// pre-loaded with the given runtime objects.
func newTestFluxReconciler(objects ...runtime.Object) *flux.Reconciler {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			kustomizationGVR: "KustomizationList",
		},
		objects...,
	)

	return &flux.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}
}

// newFakeKustomization builds an unstructured Flux Kustomization CR for testing.
func newFakeKustomization(
	name, path string,
	dependsOn []string,
	readyStatus, readyReason, readyMessage string,
) *unstructured.Unstructured {
	kust := &unstructured.Unstructured{}
	kust.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kustomize.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "Kustomization",
	})
	kust.SetName(name)
	kust.SetNamespace("flux-system")

	obj := kust.Object
	obj["spec"] = map[string]any{
		"path": path,
	}

	if len(dependsOn) > 0 {
		deps := make([]any, len(dependsOn))
		for i, d := range dependsOn {
			deps[i] = map[string]any{"name": d}
		}

		spec, _ := obj["spec"].(map[string]any)
		spec["dependsOn"] = deps
	}

	if readyStatus != "" {
		obj["status"] = map[string]any{
			"conditions": []any{
				map[string]any{
					"type":    "Ready",
					"status":  readyStatus,
					"reason":  readyReason,
					"message": readyMessage,
				},
			},
		}
	}

	return kust
}

// ---------------------------------------------------------------------------
// ListKustomizations
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven test with comprehensive cases
func TestListKustomizations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		objects    []runtime.Object
		wantInfos  []flux.KustomizationInfo
		wantErrMsg string
		unordered  bool
	}{
		{
			name:      "empty list returns no items",
			objects:   nil,
			wantInfos: []flux.KustomizationInfo{},
		},
		{
			name: "single kustomization without dependencies",
			objects: []runtime.Object{
				newFakeKustomization(
					"infra",
					"./infrastructure",
					nil,
					"True",
					"Succeeded",
					"Applied revision: v1",
				),
			},
			wantInfos: []flux.KustomizationInfo{
				{Name: "infra", Path: "./infrastructure", DependsOn: nil},
			},
		},
		{
			name: "single kustomization with dependencies",
			objects: []runtime.Object{
				newFakeKustomization(
					"apps",
					"./apps",
					[]string{"infra", "configs"},
					"True",
					"Succeeded",
					"ok",
				),
			},
			wantInfos: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps", DependsOn: []string{"infra", "configs"}},
			},
		},
		{
			name: "multiple kustomizations with mixed dependencies",
			objects: []runtime.Object{
				newFakeKustomization(
					"flux-system",
					"./clusters/my-cluster",
					nil,
					"True",
					"Succeeded",
					"ok",
				),
				newFakeKustomization(
					"infra",
					"./infrastructure",
					[]string{"flux-system"},
					"True",
					"Succeeded",
					"ok",
				),
				newFakeKustomization(
					"apps",
					"./apps",
					[]string{"infra"},
					"False",
					"Progressing",
					"reconciling",
				),
			},
			wantInfos: []flux.KustomizationInfo{
				{Name: "apps", Path: "./apps", DependsOn: []string{"infra"}},
				{Name: "flux-system", Path: "./clusters/my-cluster", DependsOn: nil},
				{Name: "infra", Path: "./infrastructure", DependsOn: []string{"flux-system"}},
			},
			unordered: true,
		},
		{
			name: "kustomization without spec.path returns empty path",
			objects: []runtime.Object{
				func() *unstructured.Unstructured {
					kust := &unstructured.Unstructured{}
					kust.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "kustomize.toolkit.fluxcd.io",
						Version: "v1",
						Kind:    "Kustomization",
					})
					kust.SetName("no-path")
					kust.SetNamespace("flux-system")
					kust.Object["spec"] = map[string]any{}

					return kust
				}(),
			},
			wantInfos: []flux.KustomizationInfo{
				{Name: "no-path", Path: "", DependsOn: nil},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := newTestFluxReconciler(testCase.objects...)

			infos, err := r.ListKustomizations(context.Background())

			if testCase.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrMsg)

				return
			}

			require.NoError(t, err)

			if testCase.unordered {
				assert.ElementsMatch(t, testCase.wantInfos, infos)
			} else {
				assert.Equal(t, testCase.wantInfos, infos)
			}
		})
	}
}

func TestListKustomizations_APIError(t *testing.T) {
	t.Parallel()

	// Build a reconciler with a fake client whose scheme has no
	// registration for the kustomization GVR. The fake dynamic client
	// returns a "the server could not find the requested resource" error
	// when listing an unknown resource. We can also trigger an error by
	// using a reactor.
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			kustomizationGVR: "KustomizationList",
		},
	)

	// Inject a list error via a reactor.
	fakeClient.PrependReactor("list", "kustomizations", func(
		_ k8stesting.Action,
	) (bool, runtime.Object, error) {
		return true, nil, errSimulatedAPIFailure
	})

	r := &flux.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}

	_, err := r.ListKustomizations(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list flux kustomizations")
	assert.Contains(t, err.Error(), "simulated API failure")
}

// ---------------------------------------------------------------------------
// CheckNamedKustomizationReady
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven test with comprehensive cases
func TestCheckNamedKustomizationReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ksName      string
		objects     []runtime.Object
		wantReady   bool
		wantStatus  string
		wantErr     bool
		wantErrMsg  string
		wantErrType error
	}{
		{
			name:   "ready kustomization",
			ksName: "infra",
			objects: []runtime.Object{
				newFakeKustomization(
					"infra",
					"./infrastructure",
					nil,
					"True",
					"Succeeded",
					"Applied revision: main@sha1:abc123",
				),
			},
			wantReady:  true,
			wantStatus: "Ready",
		},
		{
			name:   "not-ready kustomization with transient reason",
			ksName: "apps",
			objects: []runtime.Object{
				newFakeKustomization(
					"apps",
					"./apps",
					nil,
					"False",
					"Progressing",
					"reconciliation in progress",
				),
			},
			wantReady:  false,
			wantStatus: "Progressing: reconciliation in progress",
		},
		{
			name:   "not-ready kustomization with DependencyNotReady",
			ksName: "apps",
			objects: []runtime.Object{
				newFakeKustomization(
					"apps", "./apps", []string{"infra"},
					"False", "DependencyNotReady", "dependency 'infra' is not ready",
				),
			},
			wantReady:  false,
			wantStatus: "DependencyNotReady: dependency 'infra' is not ready",
		},
		{
			name:   "failed kustomization with ReconciliationFailed",
			ksName: "broken",
			objects: []runtime.Object{
				newFakeKustomization(
					"broken",
					"./broken",
					nil,
					"False",
					"ReconciliationFailed",
					"kustomize build failed",
				),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
			wantErrMsg:  "ReconciliationFailed",
		},
		{
			name:   "failed kustomization with ValidationFailed",
			ksName: "invalid",
			objects: []runtime.Object{
				newFakeKustomization(
					"invalid",
					"./invalid",
					nil,
					"False",
					"ValidationFailed",
					"validation error",
				),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
			wantErrMsg:  "ValidationFailed",
		},
		{
			name:   "failed kustomization with ArtifactFailed",
			ksName: "no-artifact",
			objects: []runtime.Object{
				newFakeKustomization(
					"no-artifact",
					"./na",
					nil,
					"False",
					"ArtifactFailed",
					"artifact not found",
				),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
			wantErrMsg:  "ArtifactFailed",
		},
		{
			name:   "failed kustomization with BuildFailed",
			ksName: "build-fail",
			objects: []runtime.Object{
				newFakeKustomization(
					"build-fail",
					"./build-fail",
					nil,
					"False",
					"BuildFailed",
					"kustomize build failed: invalid overlay",
				),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
			wantErrMsg:  "BuildFailed",
		},
		{
			name:   "failed kustomization with HealthCheckFailed",
			ksName: "health-fail",
			objects: []runtime.Object{
				newFakeKustomization(
					"health-fail",
					"./health-fail",
					nil,
					"False",
					"HealthCheckFailed",
					"health check timed out for deployment/nginx",
				),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
			wantErrMsg:  "HealthCheckFailed",
		},
		{
			name:       "not-found kustomization returns error",
			ksName:     "nonexistent",
			objects:    nil,
			wantErr:    true,
			wantErrMsg: `get flux kustomization "nonexistent"`,
		},
		{
			name:   "kustomization with no conditions yet",
			ksName: "new-ks",
			objects: []runtime.Object{
				func() *unstructured.Unstructured {
					kust := &unstructured.Unstructured{}
					kust.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "kustomize.toolkit.fluxcd.io",
						Version: "v1",
						Kind:    "Kustomization",
					})
					kust.SetName("new-ks")
					kust.SetNamespace("flux-system")
					kust.Object["spec"] = map[string]any{"path": "./new"}

					return kust
				}(),
			},
			wantReady:  false,
			wantStatus: "no conditions yet",
		},
		{
			name:   "stalled kustomization returns permanent failure",
			ksName: "stalled-ks",
			objects: []runtime.Object{
				func() *unstructured.Unstructured {
					kust := &unstructured.Unstructured{}
					kust.SetGroupVersionKind(schema.GroupVersionKind{
						Group:   "kustomize.toolkit.fluxcd.io",
						Version: "v1",
						Kind:    "Kustomization",
					})
					kust.SetName("stalled-ks")
					kust.SetNamespace("flux-system")
					kust.Object["spec"] = map[string]any{"path": "./stalled"}
					kust.Object["status"] = map[string]any{
						"conditions": []any{
							map[string]any{
								"type":    "Stalled",
								"status":  "True",
								"reason":  "StalledReason",
								"message": "resource is stalled",
							},
						},
					}

					return kust
				}(),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
			wantErrMsg:  "stalled",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := newTestFluxReconciler(testCase.objects...)

			ready, status, err := r.CheckNamedKustomizationReady(
				context.Background(),
				testCase.ksName,
			)

			if testCase.wantErr {
				require.Error(t, err)

				if testCase.wantErrType != nil {
					require.ErrorIs(t, err, testCase.wantErrType,
						"expected error wrapping %v, got: %v", testCase.wantErrType, err)
				}

				if testCase.wantErrMsg != "" {
					assert.Contains(t, err.Error(), testCase.wantErrMsg)
				}

				assert.False(t, ready, "ready should be false on error")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantReady, ready)
			assert.Equal(t, testCase.wantStatus, status)
		})
	}
}

// ---------------------------------------------------------------------------
// parseDependsOn (tested indirectly through ListKustomizations)
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven test with comprehensive edge cases
func TestListKustomizations_DependsOnEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		objects  []runtime.Object
		wantDeps []string
	}{
		{
			name: "empty dependsOn array",
			objects: []runtime.Object{
				func() *unstructured.Unstructured {
					kust := newFakeKustomization("test", "./test", nil, "", "", "")
					spec, _ := kust.Object["spec"].(map[string]any)
					spec["dependsOn"] = []any{}

					return kust
				}(),
			},
			wantDeps: nil,
		},
		{
			name: "dependsOn with empty-name entry is skipped",
			objects: []runtime.Object{
				func() *unstructured.Unstructured {
					kust := newFakeKustomization("test", "./test", nil, "", "", "")
					spec, _ := kust.Object["spec"].(map[string]any)
					spec["dependsOn"] = []any{
						map[string]any{"name": ""},
						map[string]any{"name": "valid-dep"},
					}

					return kust
				}(),
			},
			wantDeps: []string{"valid-dep"},
		},
		{
			name: "dependsOn with non-map entry is skipped",
			objects: []runtime.Object{
				func() *unstructured.Unstructured {
					kust := newFakeKustomization("test", "./test", nil, "", "", "")
					spec, _ := kust.Object["spec"].(map[string]any)
					spec["dependsOn"] = []any{
						"not-a-map",
						map[string]any{"name": "real-dep"},
					}

					return kust
				}(),
			},
			wantDeps: []string{"real-dep"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := newTestFluxReconciler(testCase.objects...)

			infos, err := r.ListKustomizations(context.Background())
			require.NoError(t, err)
			require.Len(t, infos, 1)
			assert.Equal(t, testCase.wantDeps, infos[0].DependsOn)
		})
	}
}

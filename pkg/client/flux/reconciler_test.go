package flux_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	kustomizationListKind = "KustomizationList"
	kustomizationKind     = "Kustomization"
	conditionTypeReady    = "Ready"
	conditionTypeStalled  = "Stalled"
	statusApps            = "apps"
	reasonSucceeded       = "Succeeded"
	reasonReconcilFailed  = "ReconciliationFailed"
	reasonInstallFailed   = "InstallFailed"
	reasonUpgradeFailed   = "UpgradeFailed"
	reasonRetryExhausted  = "RetryExhausted"
	statusTrue            = "True"
	statusFalse           = "False"
	chartLiteral          = "chart"
	statusConditions      = "conditions"
	statusInfra           = "infra"
	namespaceFluxSystem   = "flux-system"
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
			kustomizationGVR: kustomizationListKind,
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
		Kind:    kustomizationKind,
	})
	kust.SetName(name)
	kust.SetNamespace(namespaceFluxSystem)

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
			statusConditions: []any{
				map[string]any{
					"type":    conditionTypeReady,
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
					statusInfra,
					"./infrastructure",
					nil,
					statusTrue,
					reasonSucceeded,
					"Applied revision: v1",
				),
			},
			wantInfos: []flux.KustomizationInfo{
				{Name: statusInfra, Path: "./infrastructure", DependsOn: nil},
			},
		},
		{
			name: "single kustomization with dependencies",
			objects: []runtime.Object{
				newFakeKustomization(
					statusApps,
					"./apps",
					[]string{statusInfra, "configs"},
					statusTrue,
					reasonSucceeded,
					"ok",
				),
			},
			wantInfos: []flux.KustomizationInfo{
				{Name: statusApps, Path: "./apps", DependsOn: []string{statusInfra, "configs"}},
			},
		},
		{
			name: "multiple kustomizations with mixed dependencies",
			objects: []runtime.Object{
				newFakeKustomization(
					namespaceFluxSystem,
					"./clusters/my-cluster",
					nil,
					statusTrue,
					reasonSucceeded,
					"ok",
				),
				newFakeKustomization(
					statusInfra,
					"./infrastructure",
					[]string{namespaceFluxSystem},
					statusTrue,
					reasonSucceeded,
					"ok",
				),
				newFakeKustomization(
					statusApps,
					"./apps",
					[]string{statusInfra},
					statusFalse,
					"Progressing",
					"reconciling",
				),
			},
			wantInfos: []flux.KustomizationInfo{
				{Name: statusApps, Path: "./apps", DependsOn: []string{statusInfra}},
				{Name: namespaceFluxSystem, Path: "./clusters/my-cluster", DependsOn: nil},
				{
					Name:      statusInfra,
					Path:      "./infrastructure",
					DependsOn: []string{namespaceFluxSystem},
				},
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
						Kind:    kustomizationKind,
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
			kustomizationGVR: kustomizationListKind,
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

// newAnnotatedKust is a test helper that creates a fake Kustomization with
// the ReconcileExcludeAnnotation set to the given value.
func newAnnotatedKust(name, path, annotationValue string) *unstructured.Unstructured {
	kust := newFakeKustomization(name, path, nil, statusTrue, reasonSucceeded, "ok")
	kust.SetAnnotations(map[string]string{
		flux.ReconcileExcludeAnnotation: annotationValue,
	})

	return kust
}

func assertKustomizationExcludedFlags(
	t *testing.T,
	r *flux.Reconciler,
	wantByName map[string]bool,
) {
	t.Helper()

	infos, err := r.ListKustomizations(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, len(wantByName))

	for _, info := range infos {
		expected, ok := wantByName[info.Name]
		require.True(t, ok, "unexpected kustomization %q", info.Name)
		assert.Equal(t, expected, info.Excluded,
			"kustomization %q: expected Excluded=%v", info.Name, expected)
	}
}

func TestListKustomizations_ExcludeAnnotation_True(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		objects      []runtime.Object
		wantExcluded map[string]bool
	}{
		{
			name:         "kustomization with exclude annotation set to true",
			objects:      []runtime.Object{newAnnotatedKust("excluded", "./excluded", "true")},
			wantExcluded: map[string]bool{"excluded": true},
		},
		{
			name:         "kustomization with exclude annotation set to True (case-insensitive)",
			objects:      []runtime.Object{newAnnotatedKust("excluded", "./excluded", statusTrue)},
			wantExcluded: map[string]bool{"excluded": true},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertKustomizationExcludedFlags(
				t,
				newTestFluxReconciler(testCase.objects...),
				testCase.wantExcluded,
			)
		})
	}
}

func TestListKustomizations_ExcludeAnnotation_FalseOrAbsent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		objects      []runtime.Object
		wantExcluded map[string]bool
	}{
		{
			name:         "kustomization with exclude annotation set to false",
			objects:      []runtime.Object{newAnnotatedKust("not-excluded", "./path", "false")},
			wantExcluded: map[string]bool{"not-excluded": false},
		},
		{
			name: "kustomization without exclude annotation",
			objects: []runtime.Object{
				newFakeKustomization("normal", "./normal", nil, statusTrue, reasonSucceeded, "ok"),
			},
			wantExcluded: map[string]bool{"normal": false},
		},
		{
			name: "mixed kustomizations with and without annotation",
			objects: []runtime.Object{
				newAnnotatedKust("excluded-app", "./apps", "true"),
				newFakeKustomization(
					"included-infra",
					"./infra",
					nil,
					statusTrue,
					reasonSucceeded,
					"ok",
				),
			},
			wantExcluded: map[string]bool{
				"excluded-app":   true,
				"included-infra": false,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertKustomizationExcludedFlags(
				t,
				newTestFluxReconciler(testCase.objects...),
				testCase.wantExcluded,
			)
		})
	}
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
					statusTrue,
					reasonSucceeded,
					"Applied revision: main@sha1:abc123",
				),
			},
			wantReady:  true,
			wantStatus: conditionTypeReady,
		},
		{
			name:   "not-ready kustomization with transient reason",
			ksName: statusApps,
			objects: []runtime.Object{
				newFakeKustomization(
					statusApps,
					"./apps",
					nil,
					statusFalse,
					"Progressing",
					"reconciliation in progress",
				),
			},
			wantReady:  false,
			wantStatus: "Progressing: reconciliation in progress",
		},
		{
			name:   "not-ready kustomization with DependencyNotReady",
			ksName: statusApps,
			objects: []runtime.Object{
				newFakeKustomization(
					statusApps, "./apps", []string{"infra"},
					statusFalse, "DependencyNotReady", "dependency 'infra' is not ready",
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
					statusFalse,
					reasonReconcilFailed,
					"kustomize build failed",
				),
			},
			wantReady:   false,
			wantErr:     true,
			wantErrType: flux.ErrKustomizationFailed,
			wantErrMsg:  reasonReconcilFailed,
		},
		{
			name:   "failed kustomization with ValidationFailed",
			ksName: "invalid",
			objects: []runtime.Object{
				newFakeKustomization(
					"invalid",
					"./invalid",
					nil,
					statusFalse,
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
					statusFalse,
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
					statusFalse,
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
					statusFalse,
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
						Kind:    kustomizationKind,
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
						Kind:    kustomizationKind,
					})
					kust.SetName("stalled-ks")
					kust.SetNamespace("flux-system")
					kust.Object["spec"] = map[string]any{"path": "./stalled"}
					kust.Object["status"] = map[string]any{
						"conditions": []any{
							map[string]any{
								"type":    conditionTypeStalled,
								"status":  statusTrue,
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

// ---------------------------------------------------------------------------
// HelmRelease GVR and helpers
// ---------------------------------------------------------------------------

// helmReleaseGVR is the GroupVersionResource for Flux HelmRelease CRs.
var helmReleaseGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test-scoped constant
	Group:    "helm.toolkit.fluxcd.io",
	Version:  "v2",
	Resource: "helmreleases",
}

// newTestFluxReconcilerWithHelmReleases creates a Flux Reconciler backed by a
// fake dynamic client that supports both Kustomization and HelmRelease resources.
func newTestFluxReconcilerWithHelmReleases(objects ...runtime.Object) *flux.Reconciler {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			kustomizationGVR: kustomizationListKind,
			helmReleaseGVR:   "HelmReleaseList",
		},
		objects...,
	)

	return &flux.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}
}

// newFakeHelmRelease builds an unstructured Flux HelmRelease CR for testing.
func newFakeHelmRelease(
	name, namespace string,
	conditions []map[string]any,
) *unstructured.Unstructured {
	helmRelease := &unstructured.Unstructured{}
	helmRelease.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "helm.toolkit.fluxcd.io",
		Version: "v2",
		Kind:    "HelmRelease",
	})
	helmRelease.SetName(name)
	helmRelease.SetNamespace(namespace)

	helmRelease.Object["spec"] = map[string]any{
		chartLiteral: map[string]any{
			"spec": map[string]any{
				chartLiteral: name,
			},
		},
	}

	if len(conditions) > 0 {
		condList := make([]any, len(conditions))
		for i, c := range conditions {
			condList[i] = c
		}

		helmRelease.Object["status"] = map[string]any{
			statusConditions: condList,
		}
	}

	return helmRelease
}

// ---------------------------------------------------------------------------
// checkHelmReleaseStuck (via export_test.go)
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven test with comprehensive cases
func TestCheckHelmReleaseStuck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hr         *unstructured.Unstructured
		wantStuck  bool
		wantReason string
	}{
		{
			name: "healthy HelmRelease (Ready=True)",
			hr: newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusTrue,
					"reason":  reasonSucceeded,
					"message": "ok",
				},
			}),
			wantStuck: false,
		},
		{
			name: "stuck with InstallFailed",
			hr: newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  reasonInstallFailed,
					"message": "install retries exhausted",
				},
			}),
			wantStuck:  true,
			wantReason: reasonInstallFailed,
		},
		{
			name: "stuck with UpgradeFailed",
			hr: newFakeHelmRelease("nginx", "default", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  reasonUpgradeFailed,
					"message": "upgrade retries exhausted",
				},
			}),
			wantStuck:  true,
			wantReason: reasonUpgradeFailed,
		},
		{
			name: "stuck with ReconciliationFailed",
			hr: newFakeHelmRelease("metrics", "monitoring", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  reasonReconcilFailed,
					"message": "reconciliation failed",
				},
			}),
			wantStuck:  true,
			wantReason: reasonReconcilFailed,
		},
		{
			name: "stuck with TestFailed",
			hr: newFakeHelmRelease(statusApps, "default", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  "TestFailed",
					"message": "helm test failed",
				},
			}),
			wantStuck:  true,
			wantReason: "TestFailed",
		},
		{
			name: "stuck with RollbackFailed",
			hr: newFakeHelmRelease(statusApps, "default", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  "RollbackFailed",
					"message": "rollback failed",
				},
			}),
			wantStuck:  true,
			wantReason: "RollbackFailed",
		},
		{
			name: "stuck with UninstallFailed",
			hr: newFakeHelmRelease(statusApps, "default", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  "UninstallFailed",
					"message": "uninstall failed",
				},
			}),
			wantStuck:  true,
			wantReason: "UninstallFailed",
		},
		{
			name: "stuck with GetLastReleaseFailed",
			hr: newFakeHelmRelease(statusApps, "default", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  "GetLastReleaseFailed",
					"message": "get last release failed",
				},
			}),
			wantStuck:  true,
			wantReason: "GetLastReleaseFailed",
		},
		{
			name: "Stalled=True is stuck",
			hr: newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
				{
					"type":    conditionTypeStalled,
					"status":  statusTrue,
					"reason":  reasonRetryExhausted,
					"message": "retries exhausted",
				},
			}),
			wantStuck:  true,
			wantReason: reasonRetryExhausted,
		},
		{
			name: "DependencyNotReady is NOT stuck (transient)",
			hr: newFakeHelmRelease(statusApps, "default", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  "DependencyNotReady",
					"message": "waiting for dependency",
				},
			}),
			wantStuck: false,
		},
		{
			name: "Progressing is NOT stuck (transient)",
			hr: newFakeHelmRelease(statusApps, "default", []map[string]any{
				{
					"type":    conditionTypeReady,
					"status":  statusFalse,
					"reason":  "Progressing",
					"message": "reconciliation in progress",
				},
			}),
			wantStuck: false,
		},
		{
			name:      "no conditions is NOT stuck",
			hr:        newFakeHelmRelease(statusApps, "default", nil),
			wantStuck: false,
		},
		{
			name: "Ready=True overrides earlier failure reason",
			hr: newFakeHelmRelease(statusApps, "default", []map[string]any{
				{
					"type":    conditionTypeStalled,
					"status":  statusFalse,
					"reason":  "RecoveredFromFailure",
					"message": "stall cleared",
				},
				{
					"type":    conditionTypeReady,
					"status":  statusTrue,
					"reason":  reasonSucceeded,
					"message": "ok",
				},
			}),
			wantStuck: false,
		},
		{
			name: "intentionally suspended HelmRelease is NOT stuck",
			hr: func() *unstructured.Unstructured {
				release := newFakeHelmRelease(statusApps, "default", []map[string]any{
					{
						"type":    conditionTypeReady,
						"status":  statusFalse,
						"reason":  reasonInstallFailed,
						"message": "retries exhausted",
					},
				})

				err := unstructured.SetNestedField(release.Object, true, "spec", "suspend")
				require.NoError(t, err)

				return release
			}(),
			wantStuck: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := flux.CheckHelmReleaseStuck(testCase.hr)

			if !testCase.wantStuck {
				assert.Nil(t, result, "expected HelmRelease to NOT be stuck")

				return
			}

			require.NotNil(t, result, "expected HelmRelease to be stuck")
			assert.Equal(t, testCase.wantReason, result.Reason)
			assert.Equal(t, testCase.hr.GetName(), result.Name)
			assert.Equal(t, testCase.hr.GetNamespace(), result.Namespace)
		})
	}
}

// ---------------------------------------------------------------------------
// ListStuckHelmReleases
// ---------------------------------------------------------------------------

//nolint:funlen // table-driven test with comprehensive scenarios
func TestListStuckHelmReleases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		objects   []runtime.Object
		wantCount int
		wantNames []string
	}{
		{
			name:      "no HelmReleases returns empty",
			objects:   nil,
			wantCount: 0,
		},
		{
			name: "healthy HelmReleases are not listed",
			objects: []runtime.Object{
				newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
					{
						"type":    conditionTypeReady,
						"status":  statusTrue,
						"reason":  reasonSucceeded,
						"message": "ok",
					},
				}),
			},
			wantCount: 0,
		},
		{
			name: "stuck HelmRelease is listed",
			objects: []runtime.Object{
				newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
					{
						"type":    conditionTypeReady,
						"status":  statusFalse,
						"reason":  reasonInstallFailed,
						"message": "retries exhausted",
					},
				}),
			},
			wantCount: 1,
			wantNames: []string{"kyverno"},
		},
		{
			name: "mixed healthy and stuck across namespaces",
			objects: []runtime.Object{
				newFakeHelmRelease("healthy", "default", []map[string]any{
					{
						"type":    conditionTypeReady,
						"status":  statusTrue,
						"reason":  reasonSucceeded,
						"message": "ok",
					},
				}),
				newFakeHelmRelease("stuck-a", "kyverno", []map[string]any{
					{
						"type":    conditionTypeReady,
						"status":  statusFalse,
						"reason":  reasonUpgradeFailed,
						"message": "upgrade failed",
					},
				}),
				newFakeHelmRelease("stuck-b", "monitoring", []map[string]any{
					{
						"type":    conditionTypeStalled,
						"status":  statusTrue,
						"reason":  reasonRetryExhausted,
						"message": "retries exhausted",
					},
				}),
				newFakeHelmRelease("progressing", "default", []map[string]any{
					{
						"type":    conditionTypeReady,
						"status":  statusFalse,
						"reason":  "Progressing",
						"message": "in progress",
					},
				}),
			},
			wantCount: 2,
			wantNames: []string{"stuck-a", "stuck-b"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := newTestFluxReconcilerWithHelmReleases(testCase.objects...)

			stuck, err := r.ListStuckHelmReleases(context.Background())
			require.NoError(t, err)
			assert.Len(t, stuck, testCase.wantCount)

			if len(testCase.wantNames) > 0 {
				gotNames := make([]string, len(stuck))
				for i, s := range stuck {
					gotNames[i] = s.Name
				}

				assert.ElementsMatch(t, testCase.wantNames, gotNames)
			}
		})
	}
}

func TestListStuckHelmReleases_APIError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			helmReleaseGVR: "HelmReleaseList",
		},
	)

	fakeClient.PrependReactor("list", "helmreleases", func(
		_ k8stesting.Action,
	) (bool, runtime.Object, error) {
		return true, nil, errSimulatedAPIFailure
	})

	r := &flux.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}

	_, err := r.ListStuckHelmReleases(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list helmreleases")
}

// ---------------------------------------------------------------------------
// ResetStuckHelmReleases
// ---------------------------------------------------------------------------

func TestResetStuckHelmReleases_EmptyList(t *testing.T) {
	t.Parallel()

	reconciler := newTestFluxReconcilerWithHelmReleases()

	count, err := reconciler.ResetStuckHelmReleases(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestResetStuckHelmReleases_SuccessfulReset(t *testing.T) {
	t.Parallel()

	helmRelease := newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
		{
			"type":    conditionTypeReady,
			"status":  statusFalse,
			"reason":  reasonInstallFailed,
			"message": "retries exhausted",
		},
	})

	reconciler := newTestFluxReconcilerWithHelmReleases(helmRelease)

	releases := []flux.StuckHelmRelease{
		{Name: "kyverno", Namespace: "kyverno", Reason: reasonInstallFailed},
	}

	count, err := reconciler.ResetStuckHelmReleases(context.Background(), releases)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify the HelmRelease has spec.suspend=false after the reset cycle.
	gvr := flux.HelmReleaseGVR()

	got, err := reconciler.Dynamic.Resource(gvr).Namespace("kyverno").Get(
		context.Background(), "kyverno", metav1.GetOptions{},
	)
	require.NoError(t, err)

	suspended, _, _ := unstructured.NestedBool(got.Object, "spec", "suspend")
	assert.False(t, suspended, "spec.suspend should be false after reset")
}

func TestResetStuckHelmReleases_PartialFailure(t *testing.T) {
	t.Parallel()

	helmRelease := newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
		{
			"type":    conditionTypeReady,
			"status":  statusFalse,
			"reason":  reasonInstallFailed,
			"message": "retries exhausted",
		},
	})

	reconciler := newTestFluxReconcilerWithHelmReleases(helmRelease)

	releases := []flux.StuckHelmRelease{
		{Name: "nonexistent", Namespace: "default", Reason: reasonInstallFailed},
		{Name: "kyverno", Namespace: "kyverno", Reason: reasonInstallFailed},
	}

	count, err := reconciler.ResetStuckHelmReleases(context.Background(), releases)
	// nonexistent is not in the fake client so its suspend patch returns NotFound.
	// kyverno exists and succeeds, so exactly one release is reset.
	assert.Equal(t, 1, count)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestResetStuckHelmReleases_CancelledContext(t *testing.T) {
	t.Parallel()

	helmRelease := newFakeHelmRelease("kyverno", "kyverno", []map[string]any{
		{
			"type":    conditionTypeReady,
			"status":  statusFalse,
			"reason":  reasonInstallFailed,
			"message": "retries exhausted",
		},
	})

	reconciler := newTestFluxReconcilerWithHelmReleases(helmRelease)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	releases := []flux.StuckHelmRelease{
		{Name: "kyverno", Namespace: "kyverno", Reason: reasonInstallFailed},
	}

	// dynamicfake does not fail requests on a cancelled context, so this test
	// cannot directly verify that the resume uses a detached context. It
	// serves instead as a documentation test: with a real API server,
	// context.WithoutCancel in ResetStuckHelmReleases ensures the resume
	// phase always runs even after the parent context has been cancelled.
	// Here we at least confirm the function returns the correct count and no
	// error when called with an already-cancelled context.
	count, err := reconciler.ResetStuckHelmReleases(ctx, releases)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

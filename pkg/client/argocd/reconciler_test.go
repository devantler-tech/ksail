package argocd_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var errSimulatedAPIFailure = errors.New("simulated API failure")

// applicationGVR is the GroupVersionResource for ArgoCD Application CRs.
var applicationGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test-scoped constant
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// newTestArgoCDReconciler creates an ArgoCD Reconciler backed by a fake
// dynamic client pre-loaded with the given runtime objects.
func newTestArgoCDReconciler(objects ...runtime.Object) *argocd.Reconciler {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			applicationGVR: "ApplicationList",
		},
		objects...,
	)

	return &argocd.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}
}

// newFakeApplication builds an unstructured ArgoCD Application CR for testing.
func newFakeApplication(
	name string,
	syncStatus, healthStatus string,
) *unstructured.Unstructured {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	app.SetName(name)
	app.SetNamespace("argocd")

	if syncStatus != "" || healthStatus != "" {
		status := map[string]any{}
		if syncStatus != "" {
			status["sync"] = map[string]any{"status": syncStatus}
		}

		if healthStatus != "" {
			status["health"] = map[string]any{"status": healthStatus}
		}

		app.Object["status"] = status
	}

	return app
}

// newFakeApplicationWithOperation builds an ArgoCD Application with an operationState.
func newFakeApplicationWithOperation(
	name, operationPhase, operationMessage string,
) *unstructured.Unstructured {
	app := newFakeApplication(name, "", "")
	app.Object["status"] = map[string]any{
		"operationState": map[string]any{
			"phase":   operationPhase,
			"message": operationMessage,
		},
	}

	return app
}

// newFakeApplicationWithConditions builds an ArgoCD Application with status conditions.
func newFakeApplicationWithConditions(
	name string,
	conditions []map[string]any,
) *unstructured.Unstructured {
	app := newFakeApplication(name, "Synced", "Healthy")

	condSlice := make([]any, len(conditions))
	for i, c := range conditions {
		condSlice[i] = c
	}

	if status, ok := app.Object["status"].(map[string]any); ok {
		status["conditions"] = condSlice
	}

	return app
}

// ---------------------------------------------------------------------------
// ListApplications
// ---------------------------------------------------------------------------

func TestListApplications(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		objects   []runtime.Object
		wantInfos []argocd.ApplicationInfo
		unordered bool
	}{
		{
			name:      "empty list returns no items",
			objects:   nil,
			wantInfos: []argocd.ApplicationInfo{},
		},
		{
			name: "single application",
			objects: []runtime.Object{
				newFakeApplication("ksail", "Synced", "Healthy"),
			},
			wantInfos: []argocd.ApplicationInfo{
				{Name: "ksail"},
			},
		},
		{
			name: "multiple applications",
			objects: []runtime.Object{
				newFakeApplication("app-one", "Synced", "Healthy"),
				newFakeApplication("app-two", "OutOfSync", "Degraded"),
				newFakeApplication("app-three", "", ""),
			},
			wantInfos: []argocd.ApplicationInfo{
				{Name: "app-one"},
				{Name: "app-two"},
				{Name: "app-three"},
			},
			unordered: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := newTestArgoCDReconciler(testCase.objects...)

			infos, err := r.ListApplications(context.Background())
			require.NoError(t, err)

			if testCase.unordered {
				assert.ElementsMatch(t, testCase.wantInfos, infos)
			} else {
				assert.Equal(t, testCase.wantInfos, infos)
			}
		})
	}
}

func TestListApplications_APIError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			applicationGVR: "ApplicationList",
		},
	)

	fakeClient.PrependReactor("list", "applications", func(
		_ k8stesting.Action,
	) (bool, runtime.Object, error) {
		return true, nil, errSimulatedAPIFailure
	})

	r := &argocd.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}

	_, err := r.ListApplications(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list argocd applications")
	assert.Contains(t, err.Error(), "simulated API failure")
}

// ---------------------------------------------------------------------------
// CheckNamedApplicationReady
// ---------------------------------------------------------------------------

//nolint:funlen // Table-driven test with comprehensive cases
func TestCheckNamedApplicationReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		appName     string
		objects     []runtime.Object
		wantReady   bool
		wantErr     bool
		wantErrMsg  string
		wantErrType error
	}{
		{
			name:    "synced and healthy application is ready",
			appName: "ksail",
			objects: []runtime.Object{
				newFakeApplication("ksail", "Synced", "Healthy"),
			},
			wantReady: true,
		},
		{
			name:    "out-of-sync application is not ready",
			appName: "ksail",
			objects: []runtime.Object{
				newFakeApplication("ksail", "OutOfSync", "Healthy"),
			},
			wantReady: false,
		},
		{
			name:    "synced but degraded application is not ready",
			appName: "ksail",
			objects: []runtime.Object{
				newFakeApplication("ksail", "Synced", "Degraded"),
			},
			wantReady: false,
		},
		{
			name:    "application with no status is not ready",
			appName: "ksail",
			objects: []runtime.Object{
				newFakeApplication("ksail", "", ""),
			},
			wantReady: false,
		},
		{
			name:    "application with only sync status is not ready",
			appName: "ksail",
			objects: []runtime.Object{
				newFakeApplication("ksail", "Synced", ""),
			},
			wantReady: false,
		},
		{
			name:    "application with only health status is not ready",
			appName: "ksail",
			objects: []runtime.Object{
				newFakeApplication("ksail", "", "Healthy"),
			},
			wantReady: false,
		},
		{
			name:       "not-found application returns error",
			appName:    "nonexistent",
			objects:    nil,
			wantErr:    true,
			wantErrMsg: `get argocd application "nonexistent"`,
		},
		{
			name:    "failed operation returns ErrOperationFailed",
			appName: "failed-app",
			objects: []runtime.Object{
				newFakeApplicationWithOperation("failed-app", "Failed", "sync operation failed"),
			},
			wantErr:     true,
			wantErrType: argocd.ErrOperationFailed,
			wantErrMsg:  "sync operation failed",
		},
		{
			name:    "error operation returns ErrOperationFailed",
			appName: "error-app",
			objects: []runtime.Object{
				newFakeApplicationWithOperation("error-app", "Error", "internal error"),
			},
			wantErr:     true,
			wantErrType: argocd.ErrOperationFailed,
			wantErrMsg:  "internal error",
		},
		{
			name:    "failed operation with source error returns ErrSourceNotAvailable",
			appName: "source-err",
			objects: []runtime.Object{
				newFakeApplicationWithOperation("source-err", "Failed", "manifest unknown for tag"),
			},
			wantErr:     true,
			wantErrType: argocd.ErrSourceNotAvailable,
			wantErrMsg:  "manifest unknown",
		},
		{
			name:    "failed operation with not found returns ErrSourceNotAvailable",
			appName: "notfound-err",
			objects: []runtime.Object{
				newFakeApplicationWithOperation("notfound-err", "Error", "repository not found"),
			},
			wantErr:     true,
			wantErrType: argocd.ErrSourceNotAvailable,
			wantErrMsg:  "repository not found",
		},
		{
			name:    "running operation is not an error",
			appName: "running-app",
			objects: []runtime.Object{
				func() *unstructured.Unstructured {
					app := newFakeApplication("running-app", "OutOfSync", "Progressing")
					if status, ok := app.Object["status"].(map[string]any); ok {
						status["operationState"] = map[string]any{
							"phase":   "Running",
							"message": "syncing",
						}
					}

					return app
				}(),
			},
			wantReady: false,
		},
		{
			name:    "ComparisonError condition with source error returns ErrSourceNotAvailable",
			appName: "cond-source-err",
			objects: []runtime.Object{
				newFakeApplicationWithConditions("cond-source-err", []map[string]any{
					{
						"type":    "ComparisonError",
						"message": "failed to fetch repository: connection refused",
					},
				}),
			},
			wantErr:     true,
			wantErrType: argocd.ErrSourceNotAvailable,
			wantErrMsg:  "connection refused",
		},
		{
			name:    "SyncError condition with source error returns ErrSourceNotAvailable",
			appName: "sync-source-err",
			objects: []runtime.Object{
				newFakeApplicationWithConditions("sync-source-err", []map[string]any{
					{
						"type":    "SyncError",
						"message": "unable to resolve reference: does not exist",
					},
				}),
			},
			wantErr:     true,
			wantErrType: argocd.ErrSourceNotAvailable,
			wantErrMsg:  "does not exist",
		},
		{
			name:    "non-error condition type does not fail",
			appName: "info-cond",
			objects: []runtime.Object{
				newFakeApplicationWithConditions("info-cond", []map[string]any{
					{
						"type":    "SomeInfoCondition",
						"message": "all good",
					},
				}),
			},
			wantReady: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := newTestArgoCDReconciler(testCase.objects...)

			ready, err := r.CheckNamedApplicationReady(context.Background(), testCase.appName)

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
		})
	}
}

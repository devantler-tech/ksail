package argocd_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
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
// TriggerRefresh
// ---------------------------------------------------------------------------

// newTriggerRefreshFakeClient builds a fake dynamic client seeded with the
// given objects for TriggerRefresh tests.
func newTriggerRefreshFakeClient(
	objects ...runtime.Object,
) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()

	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			applicationGVR: "ApplicationList",
		},
		objects...,
	)
}

func TestTriggerRefresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		hardRefresh    bool
		wantAnnotation string
	}{
		{name: "normal refresh", hardRefresh: false, wantAnnotation: "normal"},
		{name: "hard refresh", hardRefresh: true, wantAnnotation: "hard"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fakeClient := newTriggerRefreshFakeClient(
				newFakeApplication("ksail", "Synced", "Healthy"),
			)
			r := &argocd.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}

			err := r.TriggerRefresh(context.Background(), testCase.hardRefresh)
			require.NoError(t, err)

			got, getErr := fakeClient.Resource(applicationGVR).
				Namespace("argocd").
				Get(context.Background(), "ksail", metav1.GetOptions{})
			require.NoError(t, getErr)
			assert.Equal(
				t,
				testCase.wantAnnotation,
				got.GetAnnotations()["argocd.argoproj.io/refresh"],
			)
		})
	}
}

func TestTriggerRefresh_ApplicationMissing(t *testing.T) {
	t.Parallel()

	fakeClient := newTriggerRefreshFakeClient()
	r := &argocd.Reconciler{Base: reconciler.NewBaseWithClient(fakeClient)}

	err := r.TriggerRefresh(context.Background(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to trigger argocd refresh")
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

// TestIsColdStartTransient pins the readiness gate that keeps a control-plane
// cold start from being reported as a permanent source failure.
//
// ArgoCD's own repo-server and redis are still coming up while the root
// Application is first compared, so the Application briefly carries a
// ComparisonError naming a connection failure. ArgoCD self-heals seconds later
// ("will retry" in its own logs), but the reconcile poll had already given up.
//
// Both states are asserted deliberately: inside the warm-up window such an
// error must be retryable, and outside it — or for a genuinely failed
// operation — it must stay terminal, so a real misconfiguration is never
// masked into a timeout.
func TestIsColdStartTransient(t *testing.T) {
	t.Parallel()

	const grace = 90 * time.Second

	client := newTestArgoCDReconciler(newFakeApplicationWithConditions("warming", []map[string]any{
		{"type": "ComparisonError", "message": "connection refused"},
	}))
	_, transportErr := client.CheckNamedApplicationReady(t.Context(), "warming")
	require.ErrorIs(t, transportErr, argocd.ErrSourceNotAvailable)

	tests := []struct {
		name    string
		err     error
		elapsed time.Duration
		want    bool
	}{
		{
			name:    "source unavailable inside the warm-up window is retryable",
			err:     fmt.Errorf("wrapped: %w", transportErr),
			elapsed: 5 * time.Second,
			want:    true,
		},
		{
			name:    "source unavailable outside the warm-up window stays terminal",
			err:     fmt.Errorf("%w: %s", argocd.ErrSourceNotAvailable, "repository not found"),
			elapsed: grace + time.Second,
			want:    false,
		},
		{
			name:    "a failed operation is never masked, even inside the window",
			err:     fmt.Errorf("%w: %s", argocd.ErrOperationFailed, "sync operation failed"),
			elapsed: time.Second,
			want:    false,
		},
		{
			name:    "an unrelated error is never masked",
			err:     errSimulatedAPIFailure,
			elapsed: time.Second,
		},
		{name: "no error is not a transient", elapsed: time.Second},
		{
			name:    "the window boundary itself is already terminal",
			err:     transportErr,
			elapsed: grace,
			want:    false,
		},
		{
			name:    "an unclassified source sentinel does not authorize retry",
			err:     argocd.ErrSourceNotAvailable,
			elapsed: time.Second,
			want:    false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := argocd.IsColdStartTransient(testCase.err, testCase.elapsed, grace)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestApplicationTransportErrors identifies retryable network failures in every error location.
func TestApplicationTransportErrors(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		"lookup registry.example: no such host",
		"read tcp: i/o timeout",
		"failed to fetch: connection refused",
		"read tcp: connection reset by peer",
		"temporary failure in name resolution",
	} {
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			assertApplicationErrorClassification(t, message, true, true)
		})
	}
}

// TestApplicationPermanentErrors keeps absence, denied access, and ambiguous failures terminal.
func TestApplicationPermanentErrors(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		message string
		source  bool
	}{
		{message: "sync hook failed: context deadline exceeded"},
		{message: "rpc error: code = DeadlineExceeded desc = context deadline exceeded"},
		{message: "failed to fetch: manifest unknown", source: true},
		{message: "repository not found", source: true},
		{message: "unable to resolve revision: does not exist", source: true},
		{message: "failed to fetch repository", source: true},
		{message: "unable to resolve revision", source: true},
		{message: "authentication required"},
		{message: "permission denied (previous attempt: connection refused)"},
		{message: "manifest unknown (previous attempt: i/o timeout)", source: true},
		{message: "invalid manifest: unknown resource kind"},
		{message: "comparison failed"},
		{message: ""},
	} {
		t.Run(testCase.message, func(t *testing.T) {
			t.Parallel()
			assertApplicationErrorClassification(t, testCase.message, false, testCase.source)
		})
	}
}

// assertApplicationErrorClassification checks readiness, sentinel compatibility, and retry bounds.
func assertApplicationErrorClassification(t *testing.T, message string, transient, source bool) {
	t.Helper()

	for _, location := range []string{"ComparisonError", "SyncError", "Failed", "Error"} {
		t.Run(location, func(t *testing.T) {
			t.Parallel()

			var app *unstructured.Unstructured
			if location == "Failed" || location == "Error" {
				app = newFakeApplicationWithOperation("test-app", location, message)
			} else {
				// Synced/Healthy fields may outlive the failed comparison.
				app = newFakeApplicationWithConditions("test-app", []map[string]any{
					{"type": location, "message": message},
				})
			}

			client := newTestArgoCDReconciler(app)
			ready, err := client.CheckNamedApplicationReady(t.Context(), "test-app")
			require.Error(t, err)
			assert.False(t, ready)
			assert.Contains(t, err.Error(), message)

			if source {
				require.ErrorIs(t, err, argocd.ErrSourceNotAvailable)
			} else {
				require.ErrorIs(t, err, argocd.ErrOperationFailed)
			}

			assert.Equal(t, transient, argocd.IsColdStartTransient(err, 0, time.Second))
			assert.False(t, argocd.IsColdStartTransient(err, time.Second, time.Second))
		})
	}
}

// TestApplicationPermanentErrorTakesPrecedence checks mixed errors in both condition orders.
func TestApplicationPermanentErrorTakesPrecedence(t *testing.T) {
	t.Parallel()

	for _, operationMessage := range []string{"", "connection refused", "manifest unknown"} {
		for _, reverse := range []bool{false, true} {
			t.Run(
				fmt.Sprintf("operation=%s/reverse=%t", operationMessage, reverse),
				func(t *testing.T) {
					t.Parallel()

					conditions := []map[string]any{
						{"type": "ComparisonError", "message": "connection refused"},
						{"type": "SyncError", "message": "manifest unknown"},
					}
					if reverse {
						conditions[0], conditions[1] = conditions[1], conditions[0]
					}

					app := newFakeApplicationWithConditions("mixed", conditions)
					if operationMessage != "" {
						require.NoError(t, unstructured.SetNestedMap(app.Object, map[string]any{
							"phase": "Error", "message": operationMessage,
						}, "status", "operationState"))
					}

					client := newTestArgoCDReconciler(app)
					ready, err := client.CheckNamedApplicationReady(t.Context(), "mixed")
					require.ErrorIs(t, err, argocd.ErrSourceNotAvailable)
					assert.False(t, ready)
					assert.Contains(t, err.Error(), "manifest unknown")
					assert.False(t, argocd.IsColdStartTransient(err, 0, time.Minute))
				},
			)
		}
	}
}

package argocd_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

type testManager struct {
	mgr       *argocd.ManagerImpl
	clientset *k8sfake.Clientset
	dyn       *dynamicfake.FakeDynamicClient
	gvr       schema.GroupVersionResource
}

func newTestManager(t *testing.T) testManager {
	t.Helper()

	clientset := k8sfake.NewClientset()
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{gvr: "ApplicationList"},
	)

	mgr := argocd.NewManager(clientset, dyn)

	return testManager{
		mgr:       mgr,
		clientset: clientset,
		dyn:       dyn,
		gvr:       gvr,
	}
}

func TestManagerEnsure_CreatesRepositorySecret(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	opts := argocd.EnsureOptions{
		RepositoryURL:   "oci://local-registry:5000/demo",
		ApplicationName: "ksail",
		TargetRevision:  "v1",
		Insecure:        true,
	}

	err := testMgr.mgr.Ensure(context.Background(), opts)
	require.NoError(t, err)

	secret, err := testMgr.clientset.CoreV1().Secrets("argocd").Get(
		context.Background(),
		"ksail-local-registry-repo",
		metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.NotNil(t, secret)
	require.Equal(t, "argocd", secret.Namespace)
	require.Equal(t, "repository", secret.Labels["argocd.argoproj.io/secret-type"])
	require.Equal(t, "oci", secretValue(secret, "type"))
	require.Equal(t, "oci://local-registry:5000/demo", secretValue(secret, "url"))
	require.Equal(t, "true", secretValue(secret, "insecureOCIForceHttp"))
}

func TestManagerEnsure_CreatesApplication(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	opts := argocd.EnsureOptions{
		RepositoryURL:   "oci://local-registry:5000/demo",
		ApplicationName: "ksail",
		TargetRevision:  "v1",
	}

	err := testMgr.mgr.Ensure(context.Background(), opts)
	require.NoError(t, err)

	app, err := testMgr.dyn.Resource(testMgr.gvr).
		Namespace("argocd").
		Get(context.Background(), "ksail", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, app)

	require.Equal(t, "argocd", app.GetNamespace())
	require.Equal(t, "ksail", app.GetName())

	require.Equal(t, "default", getNestedString(t, app, "spec", "project"))
	require.Equal(
		t,
		"oci://local-registry:5000/demo",
		getNestedString(t, app, "spec", "source", "repoURL"),
	)
	require.Equal(t, "v1", getNestedString(t, app, "spec", "source", "targetRevision"))
	// Default path is "." for OCI artifacts since manifests are at root level.
	require.Equal(t, ".", getNestedString(t, app, "spec", "source", "path"))
	require.Equal(
		t,
		"https://kubernetes.default.svc",
		getNestedString(t, app, "spec", "destination", "server"),
	)
	require.Equal(t, "default", getNestedString(t, app, "spec", "destination", "namespace"))
}

func TestManagerEnsure_UsesConfiguredSourcePath(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	opts := argocd.EnsureOptions{
		RepositoryURL:   "oci://local-registry:5000/demo",
		ApplicationName: "ksail",
		TargetRevision:  "v1",
		SourcePath:      "manifests",
	}

	err := testMgr.mgr.Ensure(context.Background(), opts)
	require.NoError(t, err)

	app, err := testMgr.dyn.Resource(testMgr.gvr).
		Namespace("argocd").
		Get(context.Background(), "ksail", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, app)

	require.Equal(t, "manifests", getNestedString(t, app, "spec", "source", "path"))
}

func TestManagerEnsure_IsIdempotentAndUpdatesTargetRevision(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "default"}},
	)
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{gvr: "ApplicationList"},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]any{
				"name":      "ksail",
				"namespace": "argocd",
			},
			"spec": map[string]any{
				"source": map[string]any{
					"repoURL":        "oci://local-registry:5000/demo",
					"targetRevision": "v1",
					"path":           "k8s",
				},
			},
		}},
	)

	mgr := argocd.NewManager(clientset, dyn)

	err := mgr.Ensure(context.Background(), argocd.EnsureOptions{
		RepositoryURL:   "oci://local-registry:5000/demo",
		ApplicationName: "ksail",
		TargetRevision:  "v1",
	})
	require.NoError(t, err)

	err = mgr.Ensure(context.Background(), argocd.EnsureOptions{
		RepositoryURL:   "oci://local-registry:5000/demo",
		ApplicationName: "ksail",
		TargetRevision:  "v2",
	})
	require.NoError(t, err)

	app, err := dyn.Resource(gvr).
		Namespace("argocd").
		Get(context.Background(), "ksail", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "v2", getNestedString(t, app, "spec", "source", "targetRevision"))
}

func secretValue(secret *corev1.Secret, key string) string {
	if secret.StringData != nil {
		if val, ok := secret.StringData[key]; ok {
			return val
		}
	}

	if secret.Data != nil {
		if val, ok := secret.Data[key]; ok {
			return string(val)
		}
	}

	return ""
}

func TestManagerUpdateTargetRevision_UpdatesTargetRevision(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	// Create an initial application
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      "ksail",
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"source": map[string]any{
				"repoURL":        "oci://local-registry:5000/demo",
				"targetRevision": "v1",
				"path":           "k8s",
			},
		},
	}}
	_, err := testMgr.dyn.Resource(testMgr.gvr).Namespace("argocd").Create(
		context.Background(),
		app,
		metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Update target revision
	err = testMgr.mgr.UpdateTargetRevision(context.Background(), argocd.UpdateTargetRevisionOptions{
		ApplicationName: "ksail",
		TargetRevision:  "v2",
	})
	require.NoError(t, err)

	// Verify the update
	updatedApp, err := testMgr.dyn.Resource(testMgr.gvr).
		Namespace("argocd").
		Get(context.Background(), "ksail", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "v2", getNestedString(t, updatedApp, "spec", "source", "targetRevision"))
}

func TestManagerUpdateTargetRevision_SetsHardRefreshAnnotation(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	// Create an initial application
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      "ksail",
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"source": map[string]any{
				"repoURL":        "oci://local-registry:5000/demo",
				"targetRevision": "v1",
				"path":           "k8s",
			},
		},
	}}
	_, err := testMgr.dyn.Resource(testMgr.gvr).Namespace("argocd").Create(
		context.Background(),
		app,
		metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Update with hard refresh
	err = testMgr.mgr.UpdateTargetRevision(context.Background(), argocd.UpdateTargetRevisionOptions{
		ApplicationName: "ksail",
		TargetRevision:  "v2",
		HardRefresh:     true,
	})
	require.NoError(t, err)

	// Verify the annotation was set
	updatedApp, err := testMgr.dyn.Resource(testMgr.gvr).
		Namespace("argocd").
		Get(context.Background(), "ksail", metav1.GetOptions{})
	require.NoError(t, err)

	annotations := updatedApp.GetAnnotations()
	require.NotNil(t, annotations)
	require.Equal(t, "hard", annotations["argocd.argoproj.io/refresh"])
}

func TestManagerUpdateTargetRevision_WorksWithoutTargetRevisionChange(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	// Create an initial application
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":      "ksail",
			"namespace": "argocd",
		},
		"spec": map[string]any{
			"source": map[string]any{
				"repoURL":        "oci://local-registry:5000/demo",
				"targetRevision": "v1",
				"path":           "k8s",
			},
		},
	}}
	_, err := testMgr.dyn.Resource(testMgr.gvr).Namespace("argocd").Create(
		context.Background(),
		app,
		metav1.CreateOptions{},
	)
	require.NoError(t, err)

	// Update without changing target revision (only hard refresh)
	err = testMgr.mgr.UpdateTargetRevision(context.Background(), argocd.UpdateTargetRevisionOptions{
		ApplicationName: "ksail",
		HardRefresh:     true,
	})
	require.NoError(t, err)

	// Verify target revision unchanged but annotation is set
	updatedApp, err := testMgr.dyn.Resource(testMgr.gvr).
		Namespace("argocd").
		Get(context.Background(), "ksail", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "v1", getNestedString(t, updatedApp, "spec", "source", "targetRevision"))

	annotations := updatedApp.GetAnnotations()
	require.NotNil(t, annotations)
	require.Equal(t, "hard", annotations["argocd.argoproj.io/refresh"])
}

func TestManagerUpdateTargetRevision_ReturnsErrorForNilContext(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	//nolint:staticcheck // SA1012: intentionally testing nil context error handling
	err := testMgr.mgr.UpdateTargetRevision(nil, argocd.UpdateTargetRevisionOptions{
		ApplicationName: "ksail",
		TargetRevision:  "v2",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "context is nil")
}

func TestManagerUpdateTargetRevision_ReturnsErrorForNonExistentApplication(t *testing.T) {
	t.Parallel()

	testMgr := newTestManager(t)

	err := testMgr.mgr.UpdateTargetRevision(
		context.Background(),
		argocd.UpdateTargetRevisionOptions{
			ApplicationName: "nonexistent",
			TargetRevision:  "v2",
		},
	)
	require.Error(t, err)
	require.ErrorContains(t, err, "get Argo CD Application")
}

func getNestedString(t *testing.T, obj *unstructured.Unstructured, fields ...string) string {
	t.Helper()

	val, found, err := unstructured.NestedString(obj.Object, fields...)
	require.NoError(t, err)
	require.True(t, found)

	return val
}

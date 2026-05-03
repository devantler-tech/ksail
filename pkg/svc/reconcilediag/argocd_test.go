package reconcilediag_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/reconcilediag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

const (
	argoprojAPIGroup   = "argoproj.io"
	argoprojAPIVersion = "v1alpha1"
)

var applicationGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test constant
	Group: argoprojAPIGroup, Version: argoprojAPIVersion, Resource: "applications",
}

func newArgoCDDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()

	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			applicationGVR: "ApplicationList",
		},
		objects...,
	)
}

func newArgoCDApp(
	name string,
	syncStatus, healthStatus, healthMessage string,
	opPhase, opMessage string,
) *unstructured.Unstructured {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group: argoprojAPIGroup, Version: argoprojAPIVersion, Kind: "Application",
	})
	app.SetName(name)
	app.SetNamespace("argocd")

	status := map[string]any{
		"sync":   map[string]any{statusField: syncStatus},
		"health": map[string]any{statusField: healthStatus},
	}

	if healthMessage != "" {
		status["health"] = map[string]any{
			statusField:  healthStatus,
			messageField: healthMessage,
		}
	}

	if opPhase != "" {
		status["operationState"] = map[string]any{
			"phase":      opPhase,
			messageField: opMessage,
		}
	}

	app.Object[statusField] = status

	return app
}

func TestArgoCDCollector_AllHealthy(t *testing.T) {
	t.Parallel()

	healthyApp := newArgoCDApp("myapp", "Synced", "Healthy", "", "", "")
	dynClient := newArgoCDDynamicClient(healthyApp)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.ArgoCDCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	assert.True(t, report.IsEmpty())
}

func TestArgoCDCollector_FailingApplication_OperationError(t *testing.T) {
	t.Parallel()

	failedApp := newArgoCDApp(
		"myapp", "OutOfSync", "Degraded", "",
		"Error", "unable to resolve reference: manifest unknown",
	)

	dynClient := newArgoCDDynamicClient(failedApp)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.ArgoCDCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())
	require.Len(t, report.Sections, 1)

	section := report.Sections[0]
	assert.Equal(t, "Failing Applications", section.Heading)
	require.Len(t, section.Resources, 1)
	assert.Equal(t, "myapp", section.Resources[0].Name)
	assert.Equal(t, "OperationState/Error", section.Resources[0].Reason)
	assert.Contains(t, section.Resources[0].Message, "manifest unknown")
}

func TestArgoCDCollector_FailingApplication_SyncStatus(t *testing.T) {
	t.Parallel()

	outOfSyncApp := newArgoCDApp(
		"infra", "OutOfSync", "Progressing", "waiting for pods",
		"", "",
	)

	dynClient := newArgoCDDynamicClient(outOfSyncApp)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.ArgoCDCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())

	section := report.Sections[0]
	require.Len(t, section.Resources, 1)
	assert.Equal(t, "OutOfSync/Progressing", section.Resources[0].Reason)
	assert.Equal(t, "waiting for pods", section.Resources[0].Message)
}

func TestArgoCDCollector_MixedApplications(t *testing.T) {
	t.Parallel()

	healthy := newArgoCDApp("frontend", "Synced", "Healthy", "", "", "")
	failing := newArgoCDApp("backend", "OutOfSync", "Degraded", "pod crash", "", "")

	dynClient := newArgoCDDynamicClient(healthy, failing)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.ArgoCDCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())

	section := report.Sections[0]
	require.Len(t, section.Resources, 1)
	assert.Equal(t, "backend", section.Resources[0].Name)
}

func TestArgoCDCollector_NoCRDs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.ArgoCDCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	assert.True(t, report.IsEmpty())
}

func TestArgoCDCollector_SkipsAppWithNoStatus(t *testing.T) {
	t.Parallel()

	// App with no status reported yet — both sync and health are empty.
	app := newArgoCDApp("pending-app", "", "", "", "", "")

	dynClient := newArgoCDDynamicClient(app)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.ArgoCDCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	// The app has not reported status and is not yet Synced+Healthy, but
	// it should be skipped rather than shown as a vague "/"-reason failure.
	assert.True(t, report.IsEmpty())
}

func TestArgoCDCollector_ApplicationWithConditionError(t *testing.T) {
	t.Parallel()

	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group: argoprojAPIGroup, Version: argoprojAPIVersion, Kind: "Application",
	})
	app.SetName("broken")
	app.SetNamespace("argocd")

	app.Object[statusField] = map[string]any{
		"sync":   map[string]any{statusField: "Unknown"},
		"health": map[string]any{statusField: "Unknown"},
		"conditions": []any{
			map[string]any{
				"type":       "ComparisonError",
				messageField: "rpc error: code = NotFound",
			},
		},
	}

	dynClient := newArgoCDDynamicClient(app)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.ArgoCDCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())

	section := report.Sections[0]
	require.Len(t, section.Resources, 1)
	assert.Equal(t, "ComparisonError", section.Resources[0].Reason)
	assert.Contains(t, section.Resources[0].Message, "rpc error")
}

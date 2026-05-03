package reconcilediag_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/reconcilediag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// Test constants shared across the reconcilediag_test package.
const (
	fluxSystemNS      = "flux-system"
	podKind           = "Pod"
	statusField       = "status"
	messageField      = "message"
	kustomizeAPIGroup = "kustomize.toolkit.fluxcd.io"
	kustomizationKind = "Kustomization"
)

// Flux GVRs used in tests.
var (
	kustomizationGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test constant
		Group: kustomizeAPIGroup, Version: "v1", Resource: "kustomizations",
	}
	helmReleaseGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test constant
		Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases",
	}
	ociRepositoryGVR = schema.GroupVersionResource{ //nolint:gochecknoglobals // test constant
		Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "ocirepositories",
	}
)

func newDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()

	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			kustomizationGVR: "KustomizationList",
			helmReleaseGVR:   "HelmReleaseList",
			ociRepositoryGVR: "OCIRepositoryList",
		},
		objects...,
	)
}

func newFluxCR(
	gvk schema.GroupVersionKind,
	name, namespace string,
	readyStatus, readyReason, readyMessage string,
) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	obj.SetNamespace(namespace)

	if readyStatus != "" {
		obj.Object[statusField] = map[string]any{
			"conditions": []any{
				map[string]any{
					"type":       "Ready",
					statusField:  readyStatus,
					"reason":     readyReason,
					messageField: readyMessage,
				},
			},
		}
	}

	return obj
}

func TestFluxCollector_AllHealthy(t *testing.T) {
	t.Parallel()

	readyKust := newFluxCR(
		schema.GroupVersionKind{
			Group:   kustomizeAPIGroup,
			Version: "v1",
			Kind:    kustomizationKind,
		},
		fluxSystemNS,
		fluxSystemNS,
		"True",
		"ReconciliationSucceeded",
		"Applied revision: main@sha1:abc",
	)

	dynClient := newDynamicClient(readyKust)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.FluxCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	assert.True(t, report.IsEmpty())
}

func TestFluxCollector_FailingKustomization(t *testing.T) {
	t.Parallel()

	failingKust := newFluxCR(
		schema.GroupVersionKind{
			Group:   kustomizeAPIGroup,
			Version: "v1",
			Kind:    kustomizationKind,
		},
		"apps",
		fluxSystemNS,
		"False",
		"HealthCheckFailed",
		"Deployment/myapp not ready after 30s",
	)

	dynClient := newDynamicClient(failingKust)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.FluxCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())
	require.Len(t, report.Sections, 3)

	kustSection := report.Sections[0]
	assert.Equal(t, "Failing Kustomizations", kustSection.Heading)
	require.Len(t, kustSection.Resources, 1)
	assert.Equal(t, "apps", kustSection.Resources[0].Name)
	assert.Equal(t, "HealthCheckFailed", kustSection.Resources[0].Reason)
	assert.Contains(t, kustSection.Resources[0].Message, "Deployment/myapp")
}

func TestFluxCollector_FailingHelmRelease(t *testing.T) {
	t.Parallel()

	failingHR := newFluxCR(
		schema.GroupVersionKind{
			Group:   "helm.toolkit.fluxcd.io",
			Version: "v2",
			Kind:    "HelmRelease",
		},
		"cert-manager",
		"cert-manager",
		"False",
		"InstallFailed",
		"timed out waiting for the condition",
	)

	dynClient := newDynamicClient(failingHR)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.FluxCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())

	hrSection := report.Sections[1]
	assert.Equal(t, "Failing HelmReleases", hrSection.Heading)
	require.Len(t, hrSection.Resources, 1)
	assert.Equal(t, "cert-manager", hrSection.Resources[0].Name)
	assert.Equal(t, "cert-manager", hrSection.Resources[0].Namespace)
}

func TestFluxCollector_FailingOCIRepository(t *testing.T) {
	t.Parallel()

	failingOCI := newFluxCR(
		schema.GroupVersionKind{
			Group:   "source.toolkit.fluxcd.io",
			Version: "v1",
			Kind:    "OCIRepository",
		},
		fluxSystemNS,
		fluxSystemNS,
		"False",
		"OCIPullFailed",
		"manifest unknown",
	)

	dynClient := newDynamicClient(failingOCI)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.FluxCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())

	ociSection := report.Sections[2]
	assert.Equal(t, "Failing OCIRepositories", ociSection.Heading)
	require.Len(t, ociSection.Resources, 1)
	assert.Equal(t, "OCIPullFailed", ociSection.Resources[0].Reason)
}

func TestFluxCollector_WarningEvents(t *testing.T) {
	t.Parallel()

	now := time.Now()

	recentEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-event-1",
			Namespace: fluxSystemNS,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      podKind,
			Name:      "kustomize-controller-abc",
			Namespace: fluxSystemNS,
		},
		Type:              "Warning",
		Reason:            "BackOff",
		Message:           "Back-off restarting failed container",
		LastTimestamp:     metav1.NewTime(now.Add(-2 * time.Minute)),
		EventTime:         metav1.NewMicroTime(now.Add(-2 * time.Minute)),
		ReportingInstance: "kubelet",
	}

	oldEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-event-old",
			Namespace: fluxSystemNS,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      podKind,
			Name:      "old-pod",
			Namespace: fluxSystemNS,
		},
		Type:              "Warning",
		Reason:            "Failed",
		Message:           "old failure",
		LastTimestamp:     metav1.NewTime(now.Add(-30 * time.Minute)),
		EventTime:         metav1.NewMicroTime(now.Add(-30 * time.Minute)),
		ReportingInstance: "kubelet",
	}

	dynClient := newDynamicClient()
	clientset := k8sfake.NewClientset(recentEvent, oldEvent)

	collector := &reconcilediag.FluxCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())

	// Only the recent event should be included.
	require.Len(t, report.Events, 1)
	assert.Equal(t, podKind, report.Events[0].Kind)
	assert.Equal(t, "kustomize-controller-abc", report.Events[0].Name)
	assert.Contains(t, report.Events[0].Message, "Back-off")
}

func TestFluxCollector_MultipleFailures(t *testing.T) {
	t.Parallel()

	failingKust1 := newFluxCR(
		schema.GroupVersionKind{
			Group:   kustomizeAPIGroup,
			Version: "v1",
			Kind:    kustomizationKind,
		},
		"infra",
		fluxSystemNS,
		"False",
		"ReconciliationFailed",
		"validation error",
	)

	failingKust2 := newFluxCR(
		schema.GroupVersionKind{
			Group:   kustomizeAPIGroup,
			Version: "v1",
			Kind:    kustomizationKind,
		},
		"apps",
		fluxSystemNS,
		"False",
		"HealthCheckFailed",
		"deployment not ready",
	)

	readyKust := newFluxCR(
		schema.GroupVersionKind{
			Group:   kustomizeAPIGroup,
			Version: "v1",
			Kind:    kustomizationKind,
		},
		fluxSystemNS,
		fluxSystemNS,
		"True",
		"ReconciliationSucceeded",
		"ok",
	)

	dynClient := newDynamicClient(failingKust1, failingKust2, readyKust)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.FluxCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	require.False(t, report.IsEmpty())

	kustSection := report.Sections[0]
	assert.Len(t, kustSection.Resources, 2)
}

func TestFluxCollector_NoCRDs(t *testing.T) {
	t.Parallel()

	// No CRDs registered — the list call will fail, but the collector should
	// handle it gracefully and return an empty report.
	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)
	clientset := k8sfake.NewClientset()

	collector := &reconcilediag.FluxCollector{
		Dynamic:   dynClient,
		Clientset: clientset,
	}

	report := collector.Collect(context.Background())
	assert.True(t, report.IsEmpty())
}

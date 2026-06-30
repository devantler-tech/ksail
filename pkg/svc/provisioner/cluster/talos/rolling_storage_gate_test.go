package talosprovisioner_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// errProber is a stub storageHealthProber error used to exercise the gate's
// transient-error tolerance.
var errProber = errors.New("prober unavailable")

func newGateProvisioner() *talosprovisioner.Provisioner {
	return talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)
}

// TestWaitForStorageHealthy_ProceedsWhenHealthy covers AC: gate enabled + volumes
// already healthy → returns immediately with no error.
func TestWaitForStorageHealthy_ProceedsWhenHealthy(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()
	prober := talosprovisioner.StorageHealthProberForTest(
		func(_ context.Context) ([]string, error) { return nil, nil },
	)

	err := prov.WaitForStorageHealthyForTest(context.Background(), prober, 5*time.Second)
	require.NoError(t, err)
}

// TestWaitForStorageHealthy_TimesOutNamingVolumes covers AC: gate enabled + volumes
// degraded → waits then times out with an error naming the stuck volumes.
func TestWaitForStorageHealthy_TimesOutNamingVolumes(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()
	prober := talosprovisioner.StorageHealthProberForTest(
		func(_ context.Context) ([]string, error) {
			return []string{"longhorn-system/pvc-a", "longhorn-system/pvc-b"}, nil
		},
	)

	err := prov.WaitForStorageHealthyForTest(context.Background(), prober, 100*time.Millisecond)
	require.Error(t, err)
	require.ErrorIs(t, err, talosprovisioner.ErrStorageHealthTimeout)
	assert.Contains(t, err.Error(), "longhorn-system/pvc-a")
	assert.Contains(t, err.Error(), "longhorn-system/pvc-b")
}

// TestWaitForStorageHealthy_TransientErrorThenTimeout asserts a prober that keeps
// erroring does not hard-fail the roll mid-poll; it keeps waiting until the timeout
// and reports the could-not-determine fallback rather than the raw prober error.
func TestWaitForStorageHealthy_TransientErrorThenTimeout(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()
	prober := talosprovisioner.StorageHealthProberForTest(
		func(_ context.Context) ([]string, error) { return nil, errProber },
	)

	err := prov.WaitForStorageHealthyForTest(context.Background(), prober, 100*time.Millisecond)
	require.Error(t, err)
	require.ErrorIs(t, err, talosprovisioner.ErrStorageHealthTimeout)
	require.NotErrorIs(t, err, errProber,
		"transient prober errors must not surface as the gate error")
	assert.Contains(t, err.Error(), "could not be determined")
}

// TestWaitForStorageHealthy_DisabledIsNoOp covers AC: gate disabled (timeout 0) or no
// backend detected (nil prober) → unchanged behaviour, returns immediately.
func TestWaitForStorageHealthy_DisabledIsNoOp(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()

	alwaysDegraded := talosprovisioner.StorageHealthProberForTest(
		func(_ context.Context) ([]string, error) { return []string{"longhorn-system/pvc-x"}, nil },
	)

	// timeout == 0 → gate disabled even with a degraded prober.
	require.NoError(t, prov.WaitForStorageHealthyForTest(context.Background(), alwaysDegraded, 0))

	// nil prober (no backend detected) → no-op even with a positive timeout.
	require.NoError(t, prov.WaitForStorageHealthyForTest(context.Background(), nil, 5*time.Second))
}

// TestStorageHealthTimeout covers the resolved gate timeout: 0 by default, the
// configured value when set via the option.
func TestStorageHealthTimeout(t *testing.T) {
	t.Parallel()

	defaultProv := talosprovisioner.NewProvisioner(nil, nil)
	assert.Equal(t, time.Duration(0), defaultProv.StorageHealthTimeoutForTest(),
		"gate is disabled by default")

	opts := talosprovisioner.NewOptions().WithStorageHealthTimeout(7 * time.Minute)
	setProv := talosprovisioner.NewProvisioner(nil, opts)
	assert.Equal(t, 7*time.Minute, setProv.StorageHealthTimeoutForTest())
}

// TestBuildStorageHealthProber_NoBackend covers the "no replicated storage backend
// detected → no prober (gate no-ops)" branch.
func TestBuildStorageHealthProber_NoBackend(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()

	built, err := prov.BuildStorageHealthProberForTest(
		context.Background(), fake.NewClientset(), "test",
	)
	require.NoError(t, err)
	assert.False(t, built, "no longhorn-system namespace → no prober built")
}

// TestLonghornDetected covers backend detection by namespace presence.
func TestLonghornDetected(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()

	withNS := fake.NewClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "longhorn-system"},
	})
	assert.True(t, prov.LonghornDetectedForTest(context.Background(), withNS),
		"longhorn-system present → detected")

	withoutNS := fake.NewClientset()
	assert.False(t, prov.LonghornDetectedForTest(context.Background(), withoutNS),
		"longhorn-system absent → not detected")
}

// TestLonghornDegradedVolumes covers the robustness classification: only "degraded"
// and "faulted" attached volumes are reported (case-insensitively); "healthy",
// "unknown", and missing-robustness (detached) volumes are skipped; the result is
// sorted.
func TestLonghornDegradedVolumes(t *testing.T) {
	t.Parallel()

	volumes := []runtime.Object{
		longhornVolume("pvc-healthy", "healthy"),
		longhornVolume("pvc-degraded", "degraded"),
		longhornVolume("pvc-faulted", "faulted"),
		longhornVolume("pvc-unknown", "unknown"),
		longhornVolume("pvc-detached", ""),
		longhornVolume("pvc-degraded-caps", "Degraded"),
	}

	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			talosprovisioner.LonghornVolumeGVRForTest(): "VolumeList",
		},
		volumes...,
	)

	got, err := talosprovisioner.LonghornDegradedVolumesForTest(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"longhorn-system/pvc-degraded",
		"longhorn-system/pvc-degraded-caps",
		"longhorn-system/pvc-faulted",
	}, got)
}

// TestLonghornDegradedVolumes_AllHealthy returns an empty set when no volume is
// degraded.
func TestLonghornDegradedVolumes_AllHealthy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			talosprovisioner.LonghornVolumeGVRForTest(): "VolumeList",
		},
		longhornVolume("pvc-1", "healthy"),
		longhornVolume("pvc-2", "healthy"),
	)

	got, err := talosprovisioner.LonghornDegradedVolumesForTest(context.Background(), client)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// longhornVolume builds an unstructured Longhorn Volume CR in the longhorn-system
// namespace with the given status.robustness (omitted when empty, simulating a
// detached volume).
func longhornVolume(name, robustness string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "longhorn.io",
		Version: "v1beta2",
		Kind:    "Volume",
	})
	obj.SetNamespace("longhorn-system")
	obj.SetName(name)

	if robustness != "" {
		_ = unstructured.SetNestedField(obj.Object, robustness, "status", "robustness")
	}

	return obj
}

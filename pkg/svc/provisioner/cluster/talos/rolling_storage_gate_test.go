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
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// errProber is a stub storageHealthProber error used to exercise the gate's
// transient-error tolerance.
var errProber = errors.New("prober unavailable")

// errStubForbidden stands in for a non-NotFound namespace lookup failure (e.g. RBAC)
// so the detection path can be exercised without a live API server.
var errStubForbidden = errors.New("forbidden")

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
// TestBuildStorageHealthProber_NoBackend covers #5688: without a detected backend
// the generic PV/PVC/VolumeAttachment prober still engages — the gate is no longer
// inert on non-Longhorn clusters.
func TestBuildStorageHealthProber_NoBackend(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()

	built, err := prov.BuildStorageHealthProberForTest(
		context.Background(), fake.NewClientset(), "test",
	)
	require.NoError(t, err)
	assert.True(t, built, "no longhorn-system namespace → generic prober still built")
}

// TestGenericDegradedVolumes covers the backend-agnostic prober (#5688): a Failed
// PV, a Lost PVC, and errored VolumeAttachments are reported (sorted, kind-prefixed);
// healthy/benign steady states — Bound/Pending claims, Bound PVs, attached or
// cleanly-detached attachments, a stale attachError on a now-attached attachment —
// are not.
func TestGenericDegradedVolumes(t *testing.T) {
	t.Parallel()

	attachErr := &storagev1.VolumeError{Message: "attach failed"}
	detachErr := &storagev1.VolumeError{Message: "detach failed"}

	clientset := fake.NewClientset(
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-failed"},
			Status:     corev1.PersistentVolumeStatus{Phase: corev1.VolumeFailed},
		},
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-bound"},
			Status:     corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-lost", Namespace: "apps"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimLost},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "data-pending", Namespace: "apps"},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		},
		&storagev1.VolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{Name: "va-attach-error"},
			Status:     storagev1.VolumeAttachmentStatus{Attached: false, AttachError: attachErr},
		},
		&storagev1.VolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{Name: "va-detach-error"},
			Status:     storagev1.VolumeAttachmentStatus{Attached: true, DetachError: detachErr},
		},
		&storagev1.VolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{Name: "va-stale-attach-error"},
			Status:     storagev1.VolumeAttachmentStatus{Attached: true, AttachError: attachErr},
		},
		&storagev1.VolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{Name: "va-healthy"},
			Status:     storagev1.VolumeAttachmentStatus{Attached: true},
		},
	)

	unhealthy, err := talosprovisioner.GenericDegradedVolumesForTest(
		context.Background(), clientset,
	)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"pv/pv-failed",
		"pvc/apps/data-lost",
		"volumeattachment/va-attach-error",
		"volumeattachment/va-detach-error",
	}, unhealthy)
}

// TestGenericDegradedVolumes_ListError covers error propagation: a failing list is
// wrapped and returned (the gate's poll loop treats it as transient).
func TestGenericDegradedVolumes_ListError(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	clientset.PrependReactor("list", "persistentvolumes",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errProber
		})

	_, err := talosprovisioner.GenericDegradedVolumesForTest(
		context.Background(), clientset,
	)

	require.ErrorIs(t, err, errProber)
}

// TestMultiDegradedVolumes covers the composed prober: union of the probers'
// degraded sets, sorted and de-duplicated; an inner error propagates.
func TestMultiDegradedVolumes(t *testing.T) {
	t.Parallel()

	union, err := talosprovisioner.MultiDegradedVolumesForTest(
		context.Background(),
		func(_ context.Context) ([]string, error) {
			return []string{"pv/shared", "longhorn-system/pvc-a"}, nil
		},
		func(_ context.Context) ([]string, error) {
			return []string{"pv/shared", "longhorn-system/pvc-b"}, nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t,
		[]string{"longhorn-system/pvc-a", "longhorn-system/pvc-b", "pv/shared"}, union)

	_, err = talosprovisioner.MultiDegradedVolumesForTest(
		context.Background(),
		func(_ context.Context) ([]string, error) { return nil, errProber },
	)
	require.ErrorIs(t, err, errProber)
}

// TestBuildStorageHealthProberOrWarn covers the graceful-degrade wrapper shared by the
// primary and autoscaler rolls: disabled gate → no prober; enabled gate with a
// detection failure → no prober (warn + disable) rather than aborting the roll.
func TestBuildStorageHealthProberOrWarn(t *testing.T) {
	t.Parallel()

	// Gate disabled (timeout 0) → never builds a prober, regardless of backend.
	disabled := newGateProvisioner()
	assert.False(t,
		disabled.BuildStorageHealthProberOrWarnForTest(
			context.Background(), fake.NewClientset(), "test"),
		"disabled gate → no prober")

	// Gate enabled but the backend lookup fails (RBAC/transient) → degrade to no
	// prober instead of failing; the warning is emitted to the (discarded) log writer.
	opts := talosprovisioner.NewOptions().WithStorageHealthTimeout(5 * time.Minute)
	enabled := talosprovisioner.NewProvisioner(nil, opts).WithLogWriter(io.Discard)

	forbidden := fake.NewClientset()
	forbidden.PrependReactor("get", "namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Resource: "namespaces"}, "longhorn-system",
				errStubForbidden)
		})
	assert.False(t,
		enabled.BuildStorageHealthProberOrWarnForTest(context.Background(), forbidden, "test"),
		"enabled gate + detection error → degrade to no prober")
}

// TestLonghornDetected covers backend detection by namespace presence: present →
// (true, nil); a clean NotFound → (false, nil); and any other lookup failure (RBAC,
// transient) → (false, error) so the gate is not silently disabled.
func TestLonghornDetected(t *testing.T) {
	t.Parallel()

	prov := newGateProvisioner()

	withNS := fake.NewClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "longhorn-system"},
	})
	detected, err := prov.LonghornDetectedForTest(context.Background(), withNS)
	require.NoError(t, err)
	assert.True(t, detected, "longhorn-system present → detected")

	withoutNS := fake.NewClientset()
	detected, err = prov.LonghornDetectedForTest(context.Background(), withoutNS)
	require.NoError(t, err, "a clean NotFound is not an error")
	assert.False(t, detected, "longhorn-system absent → not detected")

	// A non-NotFound lookup failure (e.g. RBAC withholding the namespace get) must
	// propagate, so an enabled gate disables with a warning rather than silently
	// treating the cluster as Longhorn-free.
	forbidden := fake.NewClientset()
	forbidden.PrependReactor("get", "namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Resource: "namespaces"}, "longhorn-system",
				errStubForbidden)
		})
	detected, err = prov.LonghornDetectedForTest(context.Background(), forbidden)
	require.Error(t, err, "an RBAC/transient lookup error must propagate")
	assert.False(t, detected, "detection is false when the lookup failed")
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

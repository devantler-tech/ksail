package mirror_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// ephemeralContainersSubresource is the pod subresource the injection reactors
// intercept.
const ephemeralContainersSubresource = "ephemeralcontainers"

// errUpdateFailed is a static sentinel for the UpdateEphemeralContainers-error
// reactor test.
var errUpdateFailed = errors.New("update ephemeral containers failed")

// newTapPoint builds the tap point the injection tests target: the test pod's
// sole container.
func newTapPoint() *mirror.TapPoint {
	return &mirror.TapPoint{Namespace: testNamespace, Pod: "api-0", Container: "api"}
}

// tappedPod builds a Running pod that already carries a tap ephemeral
// container, for the idempotency test.
func tappedPod() *corev1.Pod {
	pod := newPod("api-0", selectorLabels(), corev1.PodRunning)
	pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: mirror.TapContainerName},
	}}

	return pod
}

// podWithTapStatus builds a Running pod whose tap ephemeral container reports
// the given state.
func podWithTapStatus(state corev1.ContainerState) *corev1.Pod {
	pod := tappedPod()
	pod.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{{
		Name:  mirror.TapContainerName,
		State: state,
	}}

	return pod
}

func TestInjectTapDefaults(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	name, err := mirror.InjectTap(t.Context(), clientset, newTapPoint())

	require.NoError(t, err)
	assert.Equal(t, mirror.TapContainerName, name)

	pod, err := clientset.CoreV1().
		Pods(testNamespace).
		Get(t.Context(), "api-0", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, pod.Spec.EphemeralContainers, 1)

	tap := pod.Spec.EphemeralContainers[0]
	assert.Equal(t, mirror.TapContainerName, tap.Name)
	assert.Equal(t, mirror.DefaultTapImage, tap.Image)
	assert.Equal(t, []string{"sleep", "infinity"}, tap.Command)
	assert.Equal(t, "api", tap.TargetContainerName)
	assertHardenedTapSecurityContext(t, tap.SecurityContext)
}

// assertHardenedTapSecurityContext pins the read-only guarantee: the tap holds
// NET_RAW (for passive pcap capture) and nothing else.
func assertHardenedTapSecurityContext(t *testing.T, secCtx *corev1.SecurityContext) {
	t.Helper()

	require.NotNil(t, secCtx)
	require.NotNil(t, secCtx.AllowPrivilegeEscalation)
	assert.False(t, *secCtx.AllowPrivilegeEscalation)
	require.NotNil(t, secCtx.ReadOnlyRootFilesystem)
	assert.True(t, *secCtx.ReadOnlyRootFilesystem)
	require.NotNil(t, secCtx.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, secCtx.Capabilities.Drop)
	assert.Equal(t, []corev1.Capability{"NET_RAW"}, secCtx.Capabilities.Add)
	require.NotNil(t, secCtx.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, secCtx.SeccompProfile.Type)
}

func TestInjectTapOptions(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	_, err := mirror.InjectTap(
		t.Context(), clientset, newTapPoint(),
		mirror.WithTapImage("ghcr.io/example/tap:1"),
		mirror.WithTapCommand("tcpdump", "-i", "any"),
	)

	require.NoError(t, err)

	pod, err := clientset.CoreV1().
		Pods(testNamespace).
		Get(t.Context(), "api-0", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, pod.Spec.EphemeralContainers, 1)
	assert.Equal(t, "ghcr.io/example/tap:1", pod.Spec.EphemeralContainers[0].Image)
	assert.Equal(t, []string{"tcpdump", "-i", "any"}, pod.Spec.EphemeralContainers[0].Command)
}

func TestInjectTapNilPoint(t *testing.T) {
	t.Parallel()

	name, err := mirror.InjectTap(t.Context(), k8sfake.NewClientset(), nil)

	require.ErrorIs(t, err, mirror.ErrTapPointNil)
	assert.Empty(t, name)
}

func TestInjectTapPodMissing(t *testing.T) {
	t.Parallel()

	name, err := mirror.InjectTap(t.Context(), k8sfake.NewClientset(), newTapPoint())

	require.Error(t, err)
	assert.Empty(t, name)
}

func TestInjectTapAlreadyInjected(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(tappedPod())

	name, err := mirror.InjectTap(t.Context(), clientset, newTapPoint())

	require.ErrorIs(t, err, mirror.ErrTapAlreadyInjected)
	assert.Empty(t, name)
}

func TestInjectTapUpdateError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))
	clientset.PrependReactor("update", "pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() != ephemeralContainersSubresource {
				return false, nil, nil
			}

			return true, nil, errUpdateFailed
		})

	name, err := mirror.InjectTap(t.Context(), clientset, newTapPoint())

	require.ErrorIs(t, err, errUpdateFailed)
	assert.Empty(t, name)
}

// conflictError builds the 409 the API server returns when a concurrent write
// bumped the pod's resourceVersion between InjectTap's read and its update.
func conflictError() error {
	return apierrors.NewConflict(
		schema.GroupResource{Resource: "pods"}, "api-0", errUpdateFailed,
	)
}

func TestInjectTapConflictRetriesAndSucceeds(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	conflicted := false

	clientset.PrependReactor("update", "pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() != ephemeralContainersSubresource || conflicted {
				return false, nil, nil
			}

			conflicted = true

			return true, nil, conflictError()
		})

	name, err := mirror.InjectTap(t.Context(), clientset, newTapPoint())

	require.NoError(t, err)
	assert.Equal(t, mirror.TapContainerName, name)
	assert.True(t, conflicted)

	pod, err := clientset.CoreV1().
		Pods(testNamespace).
		Get(t.Context(), "api-0", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, pod.Spec.EphemeralContainers, 1)
}

func TestInjectTapConflictLoserConvergesOnAlreadyInjected(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	// The first update conflicts AND lands the winner's tap in the store, so
	// the retry's re-read must yield ErrTapAlreadyInjected, not the conflict.
	conflicted := false

	clientset.PrependReactor("update", "pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() != ephemeralContainersSubresource || conflicted {
				return false, nil, nil
			}

			conflicted = true

			err := clientset.Tracker().Update(
				corev1.SchemeGroupVersion.WithResource("pods"), tappedPod(), testNamespace,
			)
			if err != nil {
				return true, nil, fmt.Errorf("seeding winner pod: %w", err)
			}

			return true, nil, conflictError()
		})

	name, err := mirror.InjectTap(t.Context(), clientset, newTapPoint())

	require.ErrorIs(t, err, mirror.ErrTapAlreadyInjected)
	assert.Empty(t, name)
}

func TestWaitForTapNilPoint(t *testing.T) {
	t.Parallel()

	err := mirror.WaitForTap(t.Context(), k8sfake.NewClientset(), nil, time.Second)

	require.ErrorIs(t, err, mirror.ErrTapPointNil)
}

func TestWaitForTapRunning(t *testing.T) {
	t.Parallel()

	pod := podWithTapStatus(corev1.ContainerState{Running: &corev1.ContainerStateRunning{}})
	clientset := k8sfake.NewClientset(pod)

	err := mirror.WaitForTap(t.Context(), clientset, newTapPoint(), time.Second)

	require.NoError(t, err)
}

func TestWaitForTapTerminated(t *testing.T) {
	t.Parallel()

	pod := podWithTapStatus(corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 2},
	})
	clientset := k8sfake.NewClientset(pod)

	err := mirror.WaitForTap(t.Context(), clientset, newTapPoint(), time.Second)

	require.ErrorIs(t, err, mirror.ErrTapTerminated)
	assert.ErrorContains(t, err, "exit code 2")
}

func TestWaitForTapTimeout(t *testing.T) {
	t.Parallel()

	// Pod exists but never reports a tap status: the poll must give up at the
	// timeout rather than spin forever.
	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	err := mirror.WaitForTap(t.Context(), clientset, newTapPoint(), 50*time.Millisecond)

	require.Error(t, err)
	assert.NotErrorIs(t, err, mirror.ErrTapTerminated)
}

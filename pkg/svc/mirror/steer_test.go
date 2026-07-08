package mirror_test

import (
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

func TestDefaultSteerImageForVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		version string
		want    string
	}{
		"release version stamps a v-prefixed tag matching the published image": {
			version: "7.158.0",
			want:    "ghcr.io/devantler-tech/ksail-steer:v7.158.0",
		},
		"an already-v-prefixed version is not doubled": {
			version: "v7.158.0",
			want:    "ghcr.io/devantler-tech/ksail-steer:v7.158.0",
		},
		"a dev build falls back to the latest tag": {
			version: "dev",
			want:    "ghcr.io/devantler-tech/ksail-steer:latest",
		},
		"an empty version falls back to the latest tag": {
			version: "",
			want:    "ghcr.io/devantler-tech/ksail-steer:latest",
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, mirror.DefaultSteerImageForVersion(testCase.version))
		})
	}
}

// steeredPod builds a Running pod that already carries a steering ephemeral
// container, for the idempotency test.
func steeredPod() *corev1.Pod {
	pod := newPod("api-0", selectorLabels(), corev1.PodRunning)
	pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name: mirror.SteerContainerName,
		},
	}}

	return pod
}

// podWithSteerStatus builds a Running pod whose steering ephemeral container
// reports the given state.
func podWithSteerStatus(state corev1.ContainerState) *corev1.Pod {
	pod := steeredPod()
	pod.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{{
		Name:  mirror.SteerContainerName,
		State: state,
	}}

	return pod
}

// injectIntoFreshPod runs the given inject call against a fresh Running pod
// and returns the returned container name plus the single ephemeral container
// it appended — the shared skeleton of the tap and steer defaults tests.
func injectIntoFreshPod(
	t *testing.T,
	inject func(clientset *k8sfake.Clientset) (string, error),
) (string, corev1.EphemeralContainer) {
	t.Helper()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	name, err := inject(clientset)
	require.NoError(t, err)

	pod, err := clientset.CoreV1().
		Pods(testNamespace).
		Get(t.Context(), "api-0", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, pod.Spec.EphemeralContainers, 1)

	return name, pod.Spec.EphemeralContainers[0]
}

func TestInjectSteerDefaults(t *testing.T) {
	t.Parallel()

	name, steer := injectIntoFreshPod(t, func(clientset *k8sfake.Clientset) (string, error) {
		return mirror.InjectSteer(t.Context(), clientset, newTapPoint())
	})

	assert.Equal(t, mirror.SteerContainerName, name)
	assert.Equal(t, mirror.SteerContainerName, steer.Name)
	assert.Equal(t, mirror.DefaultSteerImage, steer.Image)
	assert.Equal(t, []string{"sleep", "infinity"}, steer.Command)
	assert.Equal(t, "api", steer.TargetContainerName)
	assertHardenedSteerSecurityContext(t, steer.SecurityContext)
}

// assertHardenedSteerSecurityContext pins the intercept blast radius: the
// steering agent holds NET_ADMIN (for iptables NAT rules in the pod netns) and
// nothing else — notably NOT the tap's NET_RAW.
func assertHardenedSteerSecurityContext(t *testing.T, secCtx *corev1.SecurityContext) {
	t.Helper()

	require.NotNil(t, secCtx)
	require.NotNil(t, secCtx.AllowPrivilegeEscalation)
	assert.False(t, *secCtx.AllowPrivilegeEscalation)
	require.NotNil(t, secCtx.ReadOnlyRootFilesystem)
	assert.True(t, *secCtx.ReadOnlyRootFilesystem)
	require.NotNil(t, secCtx.Capabilities)
	assert.Equal(t, []corev1.Capability{"ALL"}, secCtx.Capabilities.Drop)
	assert.Equal(t, []corev1.Capability{"NET_ADMIN"}, secCtx.Capabilities.Add)
	require.NotNil(t, secCtx.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, secCtx.SeccompProfile.Type)
}

func TestInjectSteerOptions(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	_, err := mirror.InjectSteer(
		t.Context(), clientset, newTapPoint(),
		mirror.WithSteerImage("ghcr.io/example/steer:1"),
		mirror.WithSteerCommand("ksail-steer-agent", "--port", "9090"),
	)

	require.NoError(t, err)

	pod, err := clientset.CoreV1().
		Pods(testNamespace).
		Get(t.Context(), "api-0", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, pod.Spec.EphemeralContainers, 1)
	assert.Equal(t, "ghcr.io/example/steer:1", pod.Spec.EphemeralContainers[0].Image)
	assert.Equal(
		t,
		[]string{"ksail-steer-agent", "--port", "9090"},
		pod.Spec.EphemeralContainers[0].Command,
	)
}

func TestInjectSteerNilPoint(t *testing.T) {
	t.Parallel()

	name, err := mirror.InjectSteer(t.Context(), k8sfake.NewClientset(), nil)

	require.ErrorIs(t, err, mirror.ErrTapPointNil)
	assert.Empty(t, name)
}

func TestInjectSteerAlreadyInjected(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(steeredPod())

	name, err := mirror.InjectSteer(t.Context(), clientset, newTapPoint())

	require.ErrorIs(t, err, mirror.ErrSteerAlreadyInjected)
	assert.Empty(t, name)
}

func TestInjectSteerCoexistsWithTap(t *testing.T) {
	t.Parallel()

	// Mirror and intercept are independently runnable by design (#5839): a pod
	// already carrying the tap must accept the steering agent, and the two
	// containers must keep their distinct privilege sets.
	clientset := k8sfake.NewClientset(tappedPod())

	name, err := mirror.InjectSteer(t.Context(), clientset, newTapPoint())

	require.NoError(t, err)
	assert.Equal(t, mirror.SteerContainerName, name)

	pod, err := clientset.CoreV1().
		Pods(testNamespace).
		Get(t.Context(), "api-0", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, pod.Spec.EphemeralContainers, 2)
	assert.Equal(t, mirror.TapContainerName, pod.Spec.EphemeralContainers[0].Name)
	assert.Equal(t, mirror.SteerContainerName, pod.Spec.EphemeralContainers[1].Name)
}

func TestInjectTapCoexistsWithSteer(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(steeredPod())

	name, err := mirror.InjectTap(t.Context(), clientset, newTapPoint())

	require.NoError(t, err)
	assert.Equal(t, mirror.TapContainerName, name)

	pod, err := clientset.CoreV1().
		Pods(testNamespace).
		Get(t.Context(), "api-0", metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, pod.Spec.EphemeralContainers, 2)
}

func TestInjectSteerUpdateError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))
	clientset.PrependReactor("update", "pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() != ephemeralContainersSubresource {
				return false, nil, nil
			}

			return true, nil, errUpdateFailed
		})

	name, err := mirror.InjectSteer(t.Context(), clientset, newTapPoint())

	require.ErrorIs(t, err, errUpdateFailed)
	assert.Empty(t, name)
}

func TestInjectSteerConflictRetriesAndSucceeds(t *testing.T) {
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

	name, err := mirror.InjectSteer(t.Context(), clientset, newTapPoint())

	require.NoError(t, err)
	assert.Equal(t, mirror.SteerContainerName, name)
	assert.True(t, conflicted)
}

func TestWaitForSteerNilPoint(t *testing.T) {
	t.Parallel()

	err := mirror.WaitForSteer(t.Context(), k8sfake.NewClientset(), nil, time.Second)

	require.ErrorIs(t, err, mirror.ErrTapPointNil)
}

func TestWaitForSteerRunning(t *testing.T) {
	t.Parallel()

	pod := podWithSteerStatus(corev1.ContainerState{Running: &corev1.ContainerStateRunning{}})
	clientset := k8sfake.NewClientset(pod)

	err := mirror.WaitForSteer(t.Context(), clientset, newTapPoint(), time.Second)

	require.NoError(t, err)
}

func TestWaitForSteerTerminated(t *testing.T) {
	t.Parallel()

	pod := podWithSteerStatus(corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 3},
	})
	clientset := k8sfake.NewClientset(pod)

	err := mirror.WaitForSteer(t.Context(), clientset, newTapPoint(), time.Second)

	require.ErrorIs(t, err, mirror.ErrSteerTerminated)
	assert.ErrorContains(t, err, "exit code 3")
}

func TestWaitForSteerTimeout(t *testing.T) {
	t.Parallel()

	// Pod exists but never reports a steering status: the poll must give up at
	// the timeout rather than spin forever.
	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	err := mirror.WaitForSteer(t.Context(), clientset, newTapPoint(), 50*time.Millisecond)

	require.Error(t, err)
	assert.NotErrorIs(t, err, mirror.ErrSteerTerminated)
}

func TestWaitForEphemeralContainerRetriesTransientGetError(t *testing.T) {
	t.Parallel()

	// A momentary apiserver error must not abort the wait: the poll keeps
	// going and succeeds once a later Get returns the Running pod.
	pod := podWithSteerStatus(corev1.ContainerState{Running: &corev1.ContainerStateRunning{}})
	clientset := k8sfake.NewClientset(pod)

	failed := false

	clientset.PrependReactor("get", "pods",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			if failed {
				return false, nil, nil
			}

			failed = true

			return true, nil, apierrors.NewServerTimeout(
				schema.GroupResource{Resource: "pods"}, "get", 1,
			)
		})

	err := mirror.WaitForSteer(t.Context(), clientset, newTapPoint(), time.Second)

	require.NoError(t, err)
	assert.True(t, failed, "expected the transient Get error to have fired")
}

func TestWaitForEphemeralContainerPropagatesTerminalGetError(t *testing.T) {
	t.Parallel()

	// A terminal error (here forbidden) must surface its own cause, not be
	// swallowed into a timeout.
	clientset := k8sfake.NewClientset(newPod("api-0", selectorLabels(), corev1.PodRunning))

	clientset.PrependReactor("get", "pods",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Resource: "pods"}, "api-0", errUpdateFailed,
			)
		})

	err := mirror.WaitForSteer(t.Context(), clientset, newTapPoint(), time.Second)

	require.Error(t, err)
	assert.ErrorContains(t, err, "forbidden")
}

func TestWaitForSteerIgnoresTapStatus(t *testing.T) {
	t.Parallel()

	// A pod whose TAP is running but whose steering agent has no status yet
	// must keep polling — the two flavours' statuses are independent.
	pod := podWithTapStatus(corev1.ContainerState{Running: &corev1.ContainerStateRunning{}})
	clientset := k8sfake.NewClientset(pod)

	err := mirror.WaitForSteer(t.Context(), clientset, newTapPoint(), 50*time.Millisecond)

	require.Error(t, err)
	assert.NotErrorIs(t, err, mirror.ErrSteerTerminated)
}

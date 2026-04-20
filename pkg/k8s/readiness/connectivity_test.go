package readiness_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const testClusterIP = "10.96.0.1"

func newKubernetesService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubernetes",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: testClusterIP,
		},
	}
}

func TestWaitForInClusterAPIConnectivity_NoService(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 3*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "get kubernetes service ClusterIP")
}

func TestWaitForInClusterAPIConnectivity_PodSucceeds(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(newKubernetesService())

	// Intercept pod creation to set the pod status to Succeeded immediately.
	clientset.PrependReactor(
		"create",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, isCreateAction := action.(k8stesting.CreateAction)
			if !isCreateAction {
				return false, nil, nil
			}

			pod, isPod := createAction.GetObject().(*corev1.Pod)
			if !isPod {
				return false, nil, nil
			}

			pod.Status.Phase = corev1.PodSucceeded

			return false, nil, nil // let the default reactor store it
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 15*time.Second)

	require.NoError(t, err)
}

func TestWaitForInClusterAPIConnectivity_Timeout(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(newKubernetesService())

	// Intercept pod creation to set the pod status to Failed (simulating
	// connectivity failure). The pod remains in Failed state, causing the
	// outer poll to retry until the deadline.
	clientset.PrependReactor(
		"create",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, isCreateAction := action.(k8stesting.CreateAction)
			if !isCreateAction {
				return false, nil, nil
			}

			pod, isPod := createAction.GetObject().(*corev1.Pod)
			if !isPod {
				return false, nil, nil
			}

			pod.Status.Phase = corev1.PodFailed

			return false, nil, nil
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 3*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "in-cluster API connectivity check failed")
}

func TestWaitForInClusterAPIConnectivity_PermanentCreateError(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(newKubernetesService())

	// Return a Forbidden error on pod creation to simulate a permanent RBAC failure.
	clientset.PrependReactor(
		"create",
		"pods",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Group: "", Resource: "pods"},
				connectivityPodName,
				nil,
			)
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 3*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "create connectivity check pod")
}

func TestWaitForInClusterAPIConnectivity_PermanentGetError(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(newKubernetesService())

	// Let pod creation succeed, but return Forbidden on pod Get.
	clientset.PrependReactor(
		"create",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, isCreateAction := action.(k8stesting.CreateAction)
			if !isCreateAction {
				return false, nil, nil
			}

			pod, isPod := createAction.GetObject().(*corev1.Pod)
			if !isPod {
				return false, nil, nil
			}

			pod.Status.Phase = corev1.PodPending

			return false, nil, nil
		},
	)

	clientset.PrependReactor(
		"get",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			getAction, isGetAction := action.(k8stesting.GetAction)
			if !isGetAction || getAction.GetName() != connectivityPodName {
				return false, nil, nil
			}

			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{Group: "", Resource: "pods"},
				connectivityPodName,
				nil,
			)
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 3*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "get connectivity check pod")
}

// TestWaitForInClusterAPIConnectivity_ContextErrorIsTransient verifies that
// context errors (e.g., from client-go rate limiter saturation when the
// per-attempt context expires) are treated as transient. The polling loop
// should continue retrying instead of aborting with a fatal error.
func TestWaitForInClusterAPIConnectivity_ContextErrorIsTransient(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(newKubernetesService())

	// First Get returns a context deadline error (simulating rate limiter
	// timeout from a per-attempt context expiration). Subsequent Gets
	// return a Succeeded pod, proving that polling continued.
	getCallCount := 0

	clientset.PrependReactor(
		"create",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, isCreateAction := action.(k8stesting.CreateAction)
			if !isCreateAction {
				return false, nil, nil
			}

			pod, isPod := createAction.GetObject().(*corev1.Pod)
			if !isPod {
				return false, nil, nil
			}

			pod.Status.Phase = corev1.PodSucceeded

			return false, nil, nil
		},
	)

	clientset.PrependReactor(
		"get",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			getAction, isGetAction := action.(k8stesting.GetAction)
			if !isGetAction || getAction.GetName() != connectivityPodName {
				return false, nil, nil
			}

			getCallCount++
			if getCallCount <= 2 {
				// Simulate the client-go rate limiter error:
				// "client rate limiter Wait returned an error: context deadline exceeded"
				return true, nil, fmt.Errorf(
					"client rate limiter Wait returned an error: %w",
					context.DeadlineExceeded,
				)
			}

			// After the transient errors, let the default reactor handle it
			// (returns the Succeeded pod).
			return false, nil, nil
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 15*time.Second)

	require.NoError(
		t,
		err,
		"context errors should be treated as transient; polling should continue and succeed",
	)
}

func TestConnectivityCheckPodSpec(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(newKubernetesService())

	// Make the function create a pod and immediately succeed so we can
	// inspect what was created.
	clientset.PrependReactor(
		"create",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, isCreateAction := action.(k8stesting.CreateAction)
			if !isCreateAction {
				return false, nil, nil
			}

			pod, isPod := createAction.GetObject().(*corev1.Pod)
			if !isPod {
				return false, nil, nil
			}

			// Verify pod spec
			require.Equal(t, "ksail-api-connectivity-check", pod.Name)
			require.Equal(t, "default", pod.Namespace)
			require.Equal(t, corev1.RestartPolicyNever, pod.Spec.RestartPolicy)
			require.Len(t, pod.Spec.Containers, 1)
			require.Equal(t, "busybox:1.36.1", pod.Spec.Containers[0].Image)
			require.Equal(t, corev1.PullIfNotPresent, pod.Spec.Containers[0].ImagePullPolicy)
			require.Contains(t, pod.Spec.Containers[0].Command[2], "nc -w 5 "+testClusterIP+" 443")
			require.Len(t, pod.Spec.Tolerations, 1)
			require.Equal(t, corev1.TolerationOpExists, pod.Spec.Tolerations[0].Operator)

			// Mark succeeded so the function completes
			pod.Status.Phase = corev1.PodSucceeded

			return false, nil, nil
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 15*time.Second)
	require.NoError(t, err)
}

func TestWaitForInClusterAPIConnectivity_ImagePullFailure(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		reason  string
		message string
	}{
		{
			name:    "ImagePullBackOff",
			reason:  "ImagePullBackOff",
			message: "Back-off pulling image \"busybox:1.36.1\"",
		},
		{
			name:    "ErrImagePull",
			reason:  "ErrImagePull",
			message: "failed to pull image \"busybox:1.36.1\"",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clientset := newClientsetWithImagePullFailure(testCase.reason, testCase.message)

			// Image pull failures are retried until the deadline, so use a
			// short deadline to keep the test fast.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 3*time.Second)

			require.Error(t, err)
			require.Contains(t, err.Error(), "image pull failed")
			require.Contains(t, err.Error(), testCase.reason)
			require.Contains(t, err.Error(), testCase.message)
			require.Contains(t, err.Error(), "busybox:1.36.1")
			// Should NOT contain the misleading "not reachable" message.
			require.NotContains(t, err.Error(), "not reachable from pods")
		})
	}
}

// TestWaitForInClusterAPIConnectivity_ImagePullRetrySucceeds verifies that
// transient image pull failures are retried and the connectivity check
// succeeds once the image pull succeeds on a subsequent attempt.
func TestWaitForInClusterAPIConnectivity_ImagePullRetrySucceeds(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset(newKubernetesService())

	createCount := 0

	clientset.PrependReactor(
		"create",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, isCreateAction := action.(k8stesting.CreateAction)
			if !isCreateAction {
				return false, nil, nil
			}

			pod, isPod := createAction.GetObject().(*corev1.Pod)
			if !isPod {
				return false, nil, nil
			}

			createCount++

			if createCount <= 2 {
				// First two attempts: simulate image pull failure.
				pod.Status.Phase = corev1.PodPending
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
					Name:  "check",
					Image: "busybox:1.36.1",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ErrImagePull",
							Message: "transient registry error",
						},
					},
				}}
			} else {
				// Subsequent attempts: succeed.
				pod.Status.Phase = corev1.PodSucceeded
			}

			return false, nil, nil
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := readiness.WaitForInClusterAPIConnectivity(ctx, clientset, 15*time.Second)

	require.NoError(t, err, "transient image pull failures should be retried and succeed")
}

// newClientsetWithImagePullFailure returns a fake clientset pre-configured to
// simulate an image pull failure on pod creation.
func newClientsetWithImagePullFailure(reason, message string) *fake.Clientset {
	clientset := fake.NewClientset(newKubernetesService())

	clientset.PrependReactor(
		"create",
		"pods",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			createAction, isCreateAction := action.(k8stesting.CreateAction)
			if !isCreateAction {
				return false, nil, nil
			}

			pod, isPod := createAction.GetObject().(*corev1.Pod)
			if !isPod {
				return false, nil, nil
			}

			pod.Status.Phase = corev1.PodPending
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
				Name:  "check",
				Image: "busybox:1.36.1",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  reason,
						Message: message,
					},
				},
			}}

			return false, nil, nil
		},
	)

	return clientset
}

// connectivityPodName mirrors the constant from the production code so test
// reactors can match by name.
const connectivityPodName = "ksail-api-connectivity-check"

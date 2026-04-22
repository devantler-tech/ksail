package readiness_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var (
	errDaemonSetBoom  = errors.New("boom")
	errDeploymentFail = errors.New("fail")
	errPollBoom       = errors.New("boom")
)

func expectNoError(t *testing.T, err error, description string) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s: unexpected error: %v", description, err)
	}
}

func expectErrorContains(t *testing.T, err error, substr, description string) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s: expected error containing %q but got nil", description, substr)
	}

	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("%s: expected error to contain %q, got %q", description, substr, err.Error())
	}
}

func TestWaitForDaemonSetReady(t *testing.T) {
	t.Parallel()

	t.Run("ReadyOnFirstPoll", testWaitForDaemonSetReadyReady)
	t.Run("PropagatesAPIError", testWaitForDaemonSetReadyAPIError)
	t.Run("TimesOutWhenNotReady", testWaitForDaemonSetReadyTimeout)
}

func testWaitForDaemonSetReadyReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "test-system"
		name      = "test-daemon"
	)

	client := fake.NewClientset(&appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 1,
			NumberUnavailable:      0,
			UpdatedNumberScheduled: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDaemonSetReady(ctx, client, namespace, name, 200*time.Millisecond)

	expectNoError(t, err, "waitForDaemonSetReady ready state")
}

func testWaitForDaemonSetReadyAPIError(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "observability"
		name      = "test-agent"
	)

	client := fake.NewClientset()
	client.PrependReactor(
		"get",
		"daemonsets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errDaemonSetBoom
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDaemonSetReady(ctx, client, namespace, name, 200*time.Millisecond)

	expectErrorContains(
		t,
		err,
		"failed to get daemonset observability/test-agent: boom",
		"waitForDaemonSetReady api error",
	)
}

func testWaitForDaemonSetReadyTimeout(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "networking"
		name      = "test-daemon"
	)

	client := fake.NewClientset()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDaemonSetReady(ctx, client, namespace, name, 150*time.Millisecond)

	expectErrorContains(
		t, err, "failed to poll for readiness", "waitForDaemonSetReady timeout",
	)
}

func TestWaitForNamespaceDaemonSetsReady(t *testing.T) {
	t.Parallel()

	t.Run("ReadyWhenAllDaemonSetsReady", testNamespaceDSReadyAllReady)
	t.Run("ReadyWhenNoDaemonSets", testNamespaceDSReadyEmpty)
	t.Run("SkipsZeroDesiredDaemonSets", testNamespaceDSSkipsZeroDesired)
	t.Run("TimesOutWithBlockingDaemonSetInfo", testNamespaceDSReadyTimeout)
	t.Run("PropagatesNonTransientAPIError", testNamespaceDSNonTransientError)
}

func testNamespaceDSReadyAllReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	const namespace = "kube-system"

	client := fake.NewClientset(
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "cilium", Namespace: namespace},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 2,
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 2,
			},
		},
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-proxy", Namespace: namespace},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 2,
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 2,
			},
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForNamespaceDaemonSetsReady(ctx, client, namespace, 200*time.Millisecond)

	expectNoError(t, err, "namespace daemonsets all ready")
}

func testNamespaceDSReadyEmpty(t *testing.T) {
	t.Helper()
	t.Parallel()

	client := fake.NewClientset()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForNamespaceDaemonSetsReady(
		ctx, client, "empty-ns", 200*time.Millisecond,
	)

	expectNoError(t, err, "namespace daemonsets empty")
}

func testNamespaceDSSkipsZeroDesired(t *testing.T) {
	t.Helper()
	t.Parallel()

	const namespace = "kube-system"

	client := fake.NewClientset(
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "cilium", Namespace: namespace},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 2,
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 2,
			},
		},
		// DaemonSet with node selector that matches no nodes
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "special-agent", Namespace: namespace},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 0,
				NumberUnavailable:      0,
				UpdatedNumberScheduled: 0,
			},
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForNamespaceDaemonSetsReady(ctx, client, namespace, 200*time.Millisecond)

	expectNoError(t, err, "namespace daemonsets skips zero-desired")
}

func testNamespaceDSReadyTimeout(t *testing.T) {
	t.Helper()
	t.Parallel()

	const namespace = "kube-system"

	client := fake.NewClientset(
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "cilium", Namespace: namespace},
			Status: appsv1.DaemonSetStatus{
				DesiredNumberScheduled: 3,
				NumberUnavailable:      1,
				UpdatedNumberScheduled: 2,
			},
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := readiness.WaitForNamespaceDaemonSetsReady(ctx, client, namespace, 150*time.Millisecond)

	expectErrorContains(
		t, err, "blocked by daemonset kube-system/cilium", "namespace daemonsets timeout",
	)
}

func testNamespaceDSNonTransientError(t *testing.T) {
	t.Helper()
	t.Parallel()

	const namespace = "kube-system"

	client := fake.NewClientset()
	client.PrependReactor(
		"list",
		"daemonsets",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errDaemonSetBoom
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForNamespaceDaemonSetsReady(ctx, client, namespace, 200*time.Millisecond)

	expectErrorContains(
		t,
		err,
		"failed to list daemonsets in namespace kube-system: boom",
		"namespace daemonsets non-transient error",
	)
}

func TestWaitForDeploymentReady(t *testing.T) {
	t.Parallel()

	t.Run("ReadyOnFirstPoll", testWaitForDeploymentReadyReady)
	t.Run("PropagatesAPIError", testWaitForDeploymentReadyAPIError)
	t.Run("TimesOutWhenNotReady", testWaitForDeploymentReadyTimeout)
}

func testWaitForDeploymentReadyReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "test-system"
		name      = "test-deployment"
	)

	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   1,
			AvailableReplicas: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReady(ctx, client, namespace, name, 200*time.Millisecond)

	expectNoError(t, err, "waitForDeploymentReady ready state")
}

func testWaitForDeploymentReadyAPIError(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "platform-system"
		name      = "test-operator"
	)

	client := fake.NewClientset()
	client.PrependReactor(
		"get",
		"deployments",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errDeploymentFail
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReady(ctx, client, namespace, name, 200*time.Millisecond)

	expectErrorContains(
		t,
		err,
		"failed to get deployment platform-system/test-operator: fail",
		"waitForDeploymentReady api error",
	)
}

func testWaitForDeploymentReadyTimeout(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "observability"
		name      = "test-operator"
	)

	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DeploymentStatus{
			Replicas:        2,
			UpdatedReplicas: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReady(ctx, client, namespace, name, 150*time.Millisecond)

	expectErrorContains(
		t, err, "failed to poll for readiness", "waitForDeploymentReady timeout",
	)
}

func TestPollForReadiness(t *testing.T) {
	t.Parallel()

	t.Run("ReturnsNilWhenReady", func(t *testing.T) {
		t.Parallel()

		err := pollForReadinessWithDefaultTimeout(t, func(context.Context) (bool, error) {
			return true, nil
		})

		expectNoError(t, err, "pollForReadiness success")
	})

	t.Run("WrapsErrors", func(t *testing.T) {
		t.Parallel()

		err := pollForReadinessWithDefaultTimeout(t, func(context.Context) (bool, error) {
			return false, errPollBoom
		})

		expectErrorContains(
			t,
			err,
			"failed to poll for readiness: boom",
			"pollForReadiness error wrap",
		)
	})
}

func pollForReadinessWithDefaultTimeout(
	t *testing.T,
	checker func(context.Context) (bool, error),
) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	//nolint:wrapcheck // test utility function
	return readiness.PollForReadiness(ctx, 200*time.Millisecond, checker)
}

func TestWaitForDeploymentReadyIfExists(t *testing.T) {
	t.Parallel()

	t.Run("DeploymentDoesNotExist_ReturnsNilImmediately", testIfExistsDeploymentAbsent)
	t.Run("DeploymentExistsAndReady_ReturnsNil", testIfExistsDeploymentReady)
	t.Run("DeploymentExistsNotReady_TimesOut", testIfExistsDeploymentNotReady)
	t.Run("APIError_PropagatesImmediately", testIfExistsAPIError)
}

func testIfExistsDeploymentAbsent(t *testing.T) {
	t.Helper()
	t.Parallel()

	client := fake.NewClientset()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReadyIfExists(
		ctx, client, "nonexistent-ns", "nonexistent-deploy", 200*time.Millisecond,
	)

	expectNoError(t, err, "WaitForDeploymentReadyIfExists with absent deployment")
}

func testIfExistsDeploymentReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "kubelet-serving-cert-approver"
		name      = "kubelet-serving-cert-approver"
	)

	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   1,
			AvailableReplicas: 1,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReadyIfExists(
		ctx, client, namespace, name, 200*time.Millisecond,
	)

	expectNoError(t, err, "WaitForDeploymentReadyIfExists with ready deployment")
}

func testIfExistsDeploymentNotReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	const (
		namespace = "kubelet-serving-cert-approver"
		name      = "kubelet-serving-cert-approver"
	)

	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   0,
			AvailableReplicas: 0,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReadyIfExists(
		ctx, client, namespace, name, 150*time.Millisecond,
	)

	expectErrorContains(
		t, err, "failed to poll for readiness",
		"WaitForDeploymentReadyIfExists with not-ready deployment",
	)
}

func testIfExistsAPIError(t *testing.T) {
	t.Helper()
	t.Parallel()

	client := fake.NewClientset()
	client.PrependReactor(
		"get",
		"deployments",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errDeploymentFail
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := readiness.WaitForDeploymentReadyIfExists(
		ctx, client, "test-ns", "test-deploy", 200*time.Millisecond,
	)

	expectErrorContains(
		t, err, "failed to check deployment",
		"WaitForDeploymentReadyIfExists with API error",
	)
}

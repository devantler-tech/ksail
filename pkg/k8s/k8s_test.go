package k8s_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
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

	err := k8s.WaitForDaemonSetReady(ctx, client, namespace, name, 200*time.Millisecond)

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

	err := k8s.WaitForDaemonSetReady(ctx, client, namespace, name, 200*time.Millisecond)

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

	err := k8s.WaitForDaemonSetReady(ctx, client, namespace, name, 150*time.Millisecond)

	expectErrorContains(
		t, err, "failed to poll for readiness", "waitForDaemonSetReady timeout",
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

	err := k8s.WaitForDeploymentReady(ctx, client, namespace, name, 200*time.Millisecond)

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

	err := k8s.WaitForDeploymentReady(ctx, client, namespace, name, 200*time.Millisecond)

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

	err := k8s.WaitForDeploymentReady(ctx, client, namespace, name, 150*time.Millisecond)

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
	return k8s.PollForReadiness(ctx, 200*time.Millisecond, checker)
}

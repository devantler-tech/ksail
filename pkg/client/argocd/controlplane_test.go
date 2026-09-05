package argocd_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

const argoCDNamespace = "argocd"

func readyDeployment(name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: argoCDNamespace},
		Status: appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   1,
			AvailableReplicas: 1,
		},
	}
}

func notReadyDeployment(name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: argoCDNamespace},
		Status: appsv1.DeploymentStatus{
			Replicas:          1,
			UpdatedReplicas:   0,
			AvailableReplicas: 0,
		},
	}
}

func readyStatefulSet(name string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: argoCDNamespace},
		Status: appsv1.StatefulSetStatus{
			Replicas:        1,
			ReadyReplicas:   1,
			UpdatedReplicas: 1,
			CurrentRevision: "rev-1",
			UpdateRevision:  "rev-1",
		},
	}
}

func notReadyStatefulSet(name string) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: argoCDNamespace},
		Status: appsv1.StatefulSetStatus{
			Replicas:        1,
			ReadyReplicas:   0,
			UpdatedReplicas: 0,
			CurrentRevision: "rev-1",
			UpdateRevision:  "rev-1",
		},
	}
}

func TestWaitForControlPlaneReady(t *testing.T) {
	t.Parallel()

	t.Run("AllComponentsReady", testControlPlaneAllReady)
	t.Run("AbsentComponentsTolerated", testControlPlaneAbsentTolerated)
	t.Run("PresentButNotReadyReturnsError", testControlPlaneNotReady)
	t.Run("ApplicationControllerNotReadyReturnsError", testControlPlaneAppControllerNotReady)
}

// testControlPlaneAllReady covers the healthy case: every control-plane component
// exists and is Ready, so the gate returns nil (poll may proceed immediately).
func testControlPlaneAllReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	client := fake.NewClientset(
		readyDeployment("argocd-repo-server"),
		readyDeployment("argocd-redis"),
		readyDeployment("argocd-server"),
		readyStatefulSet("argocd-application-controller"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := argocd.WaitForControlPlaneReady(ctx, client, 2*time.Second)
	if err != nil {
		t.Fatalf("expected nil for all-ready control-plane, got: %v", err)
	}
}

// testControlPlaneAbsentTolerated covers custom/renamed installs: a component that
// is not present (e.g. HA redis under a different name) is skipped, not waited on.
func testControlPlaneAbsentTolerated(t *testing.T) {
	t.Helper()
	t.Parallel()

	// Only repo-server exists; redis and server are absent.
	client := fake.NewClientset(readyDeployment("argocd-repo-server"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := argocd.WaitForControlPlaneReady(ctx, client, 2*time.Second)
	if err != nil {
		t.Fatalf("expected nil when components are absent (tolerated), got: %v", err)
	}
}

// testControlPlaneNotReady covers the transient window: a component exists but is
// not yet Ready, so the gate reports it (the caller applies the fail-open policy).
func testControlPlaneNotReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	client := fake.NewClientset(
		notReadyDeployment("argocd-repo-server"),
		readyDeployment("argocd-redis"),
		readyDeployment("argocd-server"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := argocd.WaitForControlPlaneReady(ctx, client, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error for a not-ready control-plane component")
	}

	if !strings.Contains(err.Error(), "argocd-repo-server") {
		t.Fatalf("expected error to name argocd-repo-server, got: %v", err)
	}
}

var errStubClientset = errors.New("clientset construction failed")

// TestReconcilerWaitForControlPlaneReady exercises the exported method wrapper and
// its clientset-construction seam. It mutates a package seam, so it is not run in
// parallel.
//
//nolint:paralleltest // Mutates the newControlPlaneClientset seam via export_test.go.
func TestReconcilerWaitForControlPlaneReady(t *testing.T) {
	t.Run("UsesInjectedClientset", func(t *testing.T) {
		client := fake.NewClientset(
			readyDeployment("argocd-repo-server"),
			readyDeployment("argocd-redis"),
			readyDeployment("argocd-server"),
		)

		restore := argocd.SetNewControlPlaneClientset(
			func(string) (kubernetes.Interface, error) { return client, nil },
		)
		defer restore()

		rec := argocd.NewReconcilerWithClientForTest()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := rec.WaitForControlPlaneReady(ctx, 2*time.Second)
		if err != nil {
			t.Fatalf("expected nil with injected all-ready clientset, got: %v", err)
		}
	})

	t.Run("PropagatesClientsetError", func(t *testing.T) {
		restore := argocd.SetNewControlPlaneClientset(
			func(string) (kubernetes.Interface, error) { return nil, errStubClientset },
		)
		defer restore()

		rec := argocd.NewReconcilerWithClientForTest()

		err := rec.WaitForControlPlaneReady(context.Background(), time.Second)
		if !errors.Is(err, errStubClientset) {
			t.Fatalf("expected clientset-construction error to propagate, got: %v", err)
		}
	})
}

// testControlPlaneAppControllerNotReady covers the slice-2 gap: the application
// controller (a StatefulSet, not a Deployment) is the component that writes the
// Application status the poll reads, so the gate must wait for it too. Every
// Deployment is Ready here; only the controller is still starting.
func testControlPlaneAppControllerNotReady(t *testing.T) {
	t.Helper()
	t.Parallel()

	client := fake.NewClientset(
		readyDeployment("argocd-repo-server"),
		readyDeployment("argocd-redis"),
		readyDeployment("argocd-server"),
		notReadyStatefulSet("argocd-application-controller"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := argocd.WaitForControlPlaneReady(ctx, client, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error for a not-ready application controller")
	}

	if !strings.Contains(err.Error(), "statefulset argocd/argocd-application-controller") {
		t.Fatalf("expected error to name the application-controller statefulset, got: %v", err)
	}
}

// TestControlPlaneGateBudget pins the gate's share of the reconcile timeout: half
// of it, capped at the gate maximum, so one reconcile attempt stays inside
// --timeout while the reconcile keeps at least half of its window.
func TestControlPlaneGateBudget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "small timeout yields half", timeout: 30 * time.Second, want: 15 * time.Second},
		{name: "half below the cap is used", timeout: 4 * time.Minute, want: 2 * time.Minute},
		{name: "half equal to the cap", timeout: 6 * time.Minute, want: 3 * time.Minute},
		{name: "large timeout is capped", timeout: 10 * time.Minute, want: 3 * time.Minute},
		{name: "zero timeout falls back to the cap", timeout: 0, want: 3 * time.Minute},
		{
			name:    "negative timeout falls back to the cap",
			timeout: -time.Second,
			want:    3 * time.Minute,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := argocd.ControlPlaneGateBudget(testCase.timeout)
			if got != testCase.want {
				t.Fatalf(
					"ControlPlaneGateBudget(%v) = %v, want %v",
					testCase.timeout,
					got,
					testCase.want,
				)
			}
		})
	}
}

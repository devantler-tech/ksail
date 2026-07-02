//nolint:err113 // Tests use dynamic errors to simulate transport failures
package fluxinstaller_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

const (
	operatorWaitTestTimeout  = 200 * time.Millisecond
	operatorWaitTestInterval = 20 * time.Millisecond
)

// newOperatorDeployment builds a flux-system/flux-operator Deployment with the
// given Available condition status.
func newOperatorDeployment(available corev1.ConditionStatus) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "flux-operator",
			Namespace: "flux-system",
		},
		Status: appsv1.DeploymentStatus{
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: available,
				},
			},
		},
	}
}

// stubKubernetesClient routes newKubernetesClient to a fake clientset for the
// duration of a test.
func stubKubernetesClient(t *testing.T, clientset kubernetes.Interface, err error) {
	t.Helper()

	restore := fluxinstaller.SetNewKubernetesClient(
		func(_ *rest.Config) (kubernetes.Interface, error) {
			if err != nil {
				return nil, err
			}

			return clientset, nil
		},
	)
	t.Cleanup(restore)
}

// stubQuietDiagnostics silences the pod-failure diagnostics appended on
// timeout so assertions target the wait error itself.
func stubQuietDiagnostics(t *testing.T) {
	t.Helper()

	restore := fluxinstaller.SetDiagnoseFluxPodFailures(
		func(_ context.Context, _ *rest.Config) string { return "" },
	)
	t.Cleanup(restore)
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestWaitForOperatorAvailable_Available(t *testing.T) {
	clientset := k8sfake.NewClientset(newOperatorDeployment(corev1.ConditionTrue))
	stubKubernetesClient(t, clientset, nil)

	err := fluxinstaller.WaitForOperatorAvailable(
		context.Background(), &rest.Config{},
		operatorWaitTestTimeout, operatorWaitTestInterval,
	)
	if err != nil {
		t.Fatalf("expected nil error for an Available deployment, got: %v", err)
	}
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestWaitForOperatorAvailable_NotAvailableTimesOut(t *testing.T) {
	stubQuietDiagnostics(t)

	clientset := k8sfake.NewClientset(newOperatorDeployment(corev1.ConditionFalse))
	stubKubernetesClient(t, clientset, nil)

	err := fluxinstaller.WaitForOperatorAvailable(
		context.Background(), &rest.Config{},
		operatorWaitTestTimeout, operatorWaitTestInterval,
	)
	if err == nil {
		t.Fatal("expected timeout error for a not-Available deployment")
	}

	if !strings.Contains(err.Error(), "flux-operator") {
		t.Fatalf("expected error to attribute the flux-operator phase, got: %v", err)
	}

	if !strings.Contains(err.Error(), "not yet available") {
		t.Fatalf("expected error to carry the last probe error, got: %v", err)
	}
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestWaitForOperatorAvailable_DeploymentMissingTimesOut(t *testing.T) {
	stubQuietDiagnostics(t)

	stubKubernetesClient(t, k8sfake.NewClientset(), nil)

	err := fluxinstaller.WaitForOperatorAvailable(
		context.Background(), &rest.Config{},
		operatorWaitTestTimeout, operatorWaitTestInterval,
	)
	if err == nil {
		t.Fatal("expected timeout error when the deployment does not exist")
	}

	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected error to carry the not-found probe error, got: %v", err)
	}
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestWaitForOperatorAvailable_ClientErrorTimesOut(t *testing.T) {
	stubQuietDiagnostics(t)

	clientErr := errors.New("no route to host")
	stubKubernetesClient(t, nil, clientErr)

	err := fluxinstaller.WaitForOperatorAvailable(
		context.Background(), &rest.Config{},
		operatorWaitTestTimeout, operatorWaitTestInterval,
	)
	if err == nil {
		t.Fatal("expected timeout error when the client cannot be created")
	}

	if !strings.Contains(err.Error(), "no route to host") {
		t.Fatalf("expected error to carry the client error, got: %v", err)
	}
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestWaitForOperatorAvailable_AppendsPodDiagnostics(t *testing.T) {
	restore := fluxinstaller.SetDiagnoseFluxPodFailures(
		func(_ context.Context, _ *rest.Config) string {
			return "\nPod flux-operator-abc: CrashLoopBackOff"
		},
	)
	t.Cleanup(restore)

	stubKubernetesClient(t, k8sfake.NewClientset(), nil)

	err := fluxinstaller.WaitForOperatorAvailable(
		context.Background(), &rest.Config{},
		operatorWaitTestTimeout, operatorWaitTestInterval,
	)
	if err == nil {
		t.Fatal("expected timeout error when the deployment does not exist")
	}

	if !strings.Contains(err.Error(), "CrashLoopBackOff") {
		t.Fatalf("expected error to append pod diagnostics, got: %v", err)
	}
}

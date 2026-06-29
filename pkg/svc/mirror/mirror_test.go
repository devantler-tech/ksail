package mirror_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	testNamespace = "default"
	testDeploy    = "api"
)

// selectorLabels returns the label set the test Deployment selects on. It is a
// function rather than a package var so the linter's no-globals rule is honored
// while keeping the labels defined once.
func selectorLabels() map[string]string { return map[string]string{"app": "api"} }

// errListFailed is a static sentinel for the List-error reactor test.
var errListFailed = errors.New("list pods failed")

// errGetFailed is a static sentinel for the Get-error reactor test.
var errGetFailed = errors.New("get deployment failed")

// newDeployment builds a Deployment whose selector matches selectorLabels()
// and whose pod template declares the given container names.
func newDeployment(containers ...string) *appsv1.Deployment {
	tmpl := make([]corev1.Container, 0, len(containers))
	for _, name := range containers {
		tmpl = append(tmpl, corev1.Container{Name: name})
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: testDeploy, Namespace: testNamespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels()},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: tmpl},
			},
		},
	}
}

// newPod builds a pod in testNamespace with the given name, labels, and phase.
func newPod(name string, labels map[string]string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: labels},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestResolveTarget_EmptyDeploymentName(t *testing.T) {
	t.Parallel()

	_, err := mirror.ResolveTarget(context.Background(), k8sfake.NewClientset(), testNamespace, "")
	require.ErrorIs(t, err, mirror.ErrDeploymentNameEmpty)
}

func TestResolveTarget_DeploymentNotFound(t *testing.T) {
	t.Parallel()

	_, err := mirror.ResolveTarget(
		context.Background(), k8sfake.NewClientset(), testNamespace, testDeploy,
	)
	require.ErrorIs(t, err, mirror.ErrDeploymentNotFound)
}

func TestResolveTarget_NoContainers(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newDeployment())

	_, err := mirror.ResolveTarget(context.Background(), clientset, testNamespace, testDeploy)
	require.ErrorIs(t, err, mirror.ErrDeploymentNoContainers)
}

func TestResolveTarget_NoRunningPods(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		newDeployment("app"),
		newPod("api-pending", selectorLabels(), corev1.PodPending),
	)

	_, err := mirror.ResolveTarget(context.Background(), clientset, testNamespace, testDeploy)
	require.ErrorIs(t, err, mirror.ErrNoRunningPods)
}

func TestResolveTarget_ResolvesRunningPodsAndContainers(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		newDeployment("app", "sidecar"),
		newPod("api-1", selectorLabels(), corev1.PodRunning),
		newPod("api-2", selectorLabels(), corev1.PodRunning),
		newPod("api-old", selectorLabels(), corev1.PodSucceeded),
		newPod("other", map[string]string{"app": "web"}, corev1.PodRunning),
	)

	target, err := mirror.ResolveTarget(context.Background(), clientset, testNamespace, testDeploy)
	require.NoError(t, err)

	assert.Equal(t, testNamespace, target.Namespace)
	assert.Equal(t, testDeploy, target.Deployment)
	assert.Equal(t, []string{"app", "sidecar"}, target.Containers,
		"all pod-template containers are returned in declaration order")
	assert.ElementsMatch(t, []string{"api-1", "api-2"}, target.Pods,
		"only Running pods matching the Deployment selector are returned")
}

func TestResolveTarget_ListError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(newDeployment("app"))
	clientset.PrependReactor("list", "pods",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errListFailed
		})

	_, err := mirror.ResolveTarget(context.Background(), clientset, testNamespace, testDeploy)
	require.ErrorIs(t, err, errListFailed)
}

func TestResolveTarget_GetError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	clientset.PrependReactor("get", "deployments",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errGetFailed
		})

	_, err := mirror.ResolveTarget(context.Background(), clientset, testNamespace, testDeploy)
	require.ErrorIs(t, err, errGetFailed)
}

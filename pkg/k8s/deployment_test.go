package k8s_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const autoscalerSelector = "app.kubernetes.io/instance=cluster-autoscaler"

// errListFailed is a static sentinel for the List-error reactor test.
var errListFailed = errors.New("list deployments failed")

// newLabeledDeployment builds a Deployment in kube-system carrying the
// cluster-autoscaler instance label, optionally pre-seeded with pod-template
// annotations so tests can assert they are preserved.
func newLabeledDeployment(name string, templateAnnotations map[string]string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "kube-system",
			Labels:    map[string]string{"app.kubernetes.io/instance": "cluster-autoscaler"},
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: templateAnnotations},
			},
		},
	}
}

func TestRolloutRestartDeploymentsByLabel_StampsAnnotation(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(
		newLabeledDeployment("cluster-autoscaler", map[string]string{"keep": "me"}),
	)

	restarted, err := k8s.RolloutRestartDeploymentsByLabel(
		context.Background(), clientset, "kube-system", autoscalerSelector,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, restarted)

	deployment, err := clientset.AppsV1().Deployments("kube-system").Get(
		context.Background(), "cluster-autoscaler", metav1.GetOptions{},
	)
	require.NoError(t, err)

	annotations := deployment.Spec.Template.Annotations
	assert.NotEmpty(t, annotations[k8s.RolloutRestartAnnotation],
		"restart annotation must be stamped so the Deployment controller recreates the pods")
	assert.Equal(t, "me", annotations["keep"],
		"existing pod-template annotations must be preserved")
}

func TestRolloutRestartDeploymentsByLabel_NoMatchIsNotError(t *testing.T) {
	t.Parallel()

	// A Deployment that does not carry the selector label must be left untouched
	// and must not cause an error (the autoscaler may simply not be installed).
	clientset := k8sfake.NewClientset(newLabeledDeployment("other", nil))

	restarted, err := k8s.RolloutRestartDeploymentsByLabel(
		context.Background(), clientset, "kube-system",
		"app.kubernetes.io/instance=does-not-exist",
	)
	require.NoError(t, err)
	assert.Equal(t, 0, restarted)
}

func TestRolloutRestartDeploymentsByLabel_ListError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	clientset.PrependReactor(
		"list",
		"deployments",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errListFailed
		},
	)

	restarted, err := k8s.RolloutRestartDeploymentsByLabel(
		context.Background(), clientset, "kube-system", autoscalerSelector,
	)
	require.ErrorIs(t, err, errListFailed)
	assert.Equal(t, 0, restarted)
}

package talosprovisioner_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// errRestartList is a static sentinel for the list-error reactor test.
var errRestartList = errors.New("list deployments failed")

// TestRestartAutoscalerAfterConfigChange_RestartsWhenRunning verifies a running
// autoscaler Deployment is rolled — its pod template gets the restart annotation —
// and the success line is logged.
func TestRestartAutoscalerAfterConfigChange_RestartsWhenRunning(t *testing.T) {
	t.Parallel()

	const name, namespace = "cluster-autoscaler", "kube-system"

	var logBuf bytes.Buffer

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(&logBuf)

	clientset := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app.kubernetes.io/instance": name},
		},
	})

	err := prov.RestartAutoscalerAfterConfigChangeForTest(context.Background(), clientset)
	require.NoError(t, err)
	assert.Contains(t, logBuf.String(), "Restarted cluster-autoscaler")

	deployment, err := clientset.AppsV1().Deployments(namespace).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.NotEmpty(t, deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"],
		"the autoscaler Deployment must be stamped with the rollout-restart annotation")
}

// TestRestartAutoscalerAfterConfigChange_NoopWhenNotInstalled verifies a missing
// autoscaler Deployment is not an error and is reported as a no-op (the autoscaler
// may simply not be installed yet).
func TestRestartAutoscalerAfterConfigChange_NoopWhenNotInstalled(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(&logBuf)

	err := prov.RestartAutoscalerAfterConfigChangeForTest(
		context.Background(), fake.NewClientset(),
	)
	require.NoError(t, err)
	assert.Contains(t, logBuf.String(), "not running")
}

// TestRestartAutoscalerAfterConfigChange_SurfacesListError verifies an API error
// while finding the Deployment is wrapped and returned.
func TestRestartAutoscalerAfterConfigChange_SurfacesListError(t *testing.T) {
	t.Parallel()

	clientset := fake.NewClientset()
	clientset.PrependReactor(
		"list",
		"deployments",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errRestartList
		},
	)

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(&bytes.Buffer{})

	err := prov.RestartAutoscalerAfterConfigChangeForTest(context.Background(), clientset)
	require.ErrorIs(t, err, errRestartList)
}

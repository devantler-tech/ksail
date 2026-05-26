package k3dprovisioner_test

import (
	"context"
	"testing"

	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestBuildClusterCR_ServerArgs(t *testing.T) {
	t.Parallel()

	t.Run("propagates_server_args_into_cluster_spec", func(t *testing.T) {
		t.Parallel()

		serverArgs := k3dconfigmanager.APIServerFeatureGatesArgs()
		provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
			ServerArgs: serverArgs,
		})
		require.NoError(t, err)

		cluster := provisioner.BuildClusterCRForTest("test", "k3k-test", "10.0.0.1")

		assert.Equal(t, serverArgs, cluster.Spec.ServerArgs)
	})

	t.Run("omits_server_args_when_none_configured", func(t *testing.T) {
		t.Parallel()

		provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{})
		require.NoError(t, err)

		cluster := provisioner.BuildClusterCRForTest("test", "k3k-test", "10.0.0.1")

		assert.Nil(t, cluster.Spec.ServerArgs)
	})
}

func TestEnsureNamespace_LabelsPrivilegedPodSecurity(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
		HostClientset: clientset,
		ClusterName:   "nested-k3s",
	})
	require.NoError(t, err)

	err = provisioner.EnsureNamespaceForTest(context.Background(), "k3k-nested-k3s")
	require.NoError(t, err)

	namespace, err := clientset.CoreV1().Namespaces().Get(
		context.Background(), "k3k-nested-k3s", metav1.GetOptions{},
	)
	require.NoError(t, err)

	// The k3k server is privileged; the namespace must opt into the privileged Pod Security
	// standard so hosts that default to "baseline" (e.g. Talos) do not reject the server pod.
	assert.Equal(t, "privileged", namespace.Labels["pod-security.kubernetes.io/enforce"])
}

func TestBuildClusterCR_Version(t *testing.T) {
	t.Parallel()

	t.Run("pins_version_when_configured", func(t *testing.T) {
		t.Parallel()

		provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
			K3sVersion: "v1.36.1-k3s1",
		})
		require.NoError(t, err)

		cluster := provisioner.BuildClusterCRForTest("test", "k3k-test", "10.0.0.1")

		assert.Equal(t, "v1.36.1-k3s1", cluster.Spec.Version)
	})

	t.Run("leaves_version_empty_to_inherit_host", func(t *testing.T) {
		t.Parallel()

		provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{})
		require.NoError(t, err)

		cluster := provisioner.BuildClusterCRForTest("test", "k3k-test", "10.0.0.1")

		assert.Empty(t, cluster.Spec.Version)
	})
}

func TestFirstRunningPodName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pods     []corev1.Pod
		expected string
	}{
		{
			name:     "no pods",
			pods:     nil,
			expected: "",
		},
		{
			name: "no running pods",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "p1"},
					Status:     corev1.PodStatus{Phase: corev1.PodPending},
				},
			},
			expected: "",
		},
		{
			name: "returns first running pod",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pending"},
					Status:     corev1.PodStatus{Phase: corev1.PodPending},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "running-a"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "running-b"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expected: "running-a",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := k3dprovisioner.FirstRunningPodNameForTest(testCase.pods)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

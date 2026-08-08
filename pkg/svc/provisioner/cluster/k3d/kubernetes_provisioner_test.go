package k3dprovisioner_test

import (
	"context"
	"os"
	"testing"
	"time"

	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

// The k3k provisioner must satisfy the operator's Connector capability so component installs
// run against k3k-provisioned K3s children (#5732).
var _ clusterprovisioner.Connector = (*k3dprovisioner.K3kProvisioner)(nil)

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

func TestK3kReadyTimeout(t *testing.T) {
	// Uses t.Setenv, so it cannot run in parallel.
	t.Run("defaults to ten minutes when unset", func(t *testing.T) {
		t.Setenv("KSAIL_NESTED_READY_TIMEOUT", "placeholder")
		require.NoError(t, os.Unsetenv("KSAIL_NESTED_READY_TIMEOUT"))

		assert.Equal(t, 10*time.Minute, k3dprovisioner.K3kReadyTimeoutForTest())
	})

	t.Run("honors KSAIL_NESTED_READY_TIMEOUT override", func(t *testing.T) {
		t.Setenv("KSAIL_NESTED_READY_TIMEOUT", "15m")

		assert.Equal(t, 15*time.Minute, k3dprovisioner.K3kReadyTimeoutForTest())
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

func TestConnectionFor(t *testing.T) {
	t.Parallel()

	conn := k3dprovisioner.ConnectionFor("nested-k3s")

	assert.Equal(t, "k3k-nested-k3s", conn.Namespace)
	assert.Equal(t, "k3k-nested-k3s-kubeconfig", conn.SecretName)
	assert.Equal(t, "https://k3k-nested-k3s-service.k3k-nested-k3s:443", conn.Endpoint)
}

// k3kPublishedKubeconfig is a minimal valid kubeconfig as the k3k operator publishes it — the
// server points at the CR's first TLS SAN (127.0.0.1), unreachable from an operator pod.
const k3kPublishedKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
users:
- name: default
  user: {}
`

func newK3kProvisionerWithClientset(
	t *testing.T, clientset *k8sfake.Clientset,
) *k3dprovisioner.K3kProvisioner {
	t.Helper()

	provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
		HostClientset: clientset,
		ClusterName:   "nested-k3s",
	})
	require.NoError(t, err)

	return provisioner
}

func TestKubeconfig_NotReadyWhileSecretUnpublished(t *testing.T) {
	t.Parallel()

	provisioner := newK3kProvisionerWithClientset(t, k8sfake.NewClientset())

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_NotReadyWhileSecretKeyEmpty(t *testing.T) {
	t.Parallel()

	conn := k3dprovisioner.ConnectionFor("nested-k3s")
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
	})
	provisioner := newK3kProvisionerWithClientset(t, clientset)

	_, err := provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrKubeconfigNotReady)
}

func TestKubeconfig_RewritesServerToInClusterServiceEndpoint(t *testing.T) {
	t.Parallel()

	conn := k3dprovisioner.ConnectionFor("nested-k3s")
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": []byte(k3kPublishedKubeconfig)},
	})
	provisioner := newK3kProvisionerWithClientset(t, clientset)

	out, err := provisioner.Kubeconfig(context.Background(), "")
	require.NoError(t, err)

	config, err := clientcmd.Load(out)
	require.NoError(t, err)
	require.NotEmpty(t, config.Clusters)

	for _, cluster := range config.Clusters {
		assert.Equal(t, conn.Endpoint, cluster.Server)
	}
}

func TestKubeconfig_PrefersCallerSuppliedName(t *testing.T) {
	t.Parallel()

	safeConn := k3dprovisioner.ConnectionFor("safe-namespace-qualified")
	configuredConn := k3dprovisioner.ConnectionFor("configured-from-cli-context")
	clientset := k8sfake.NewClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: safeConn.SecretName, Namespace: safeConn.Namespace},
			Data:       map[string][]byte{"kubeconfig.yaml": []byte(k3kPublishedKubeconfig)},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configuredConn.SecretName,
				Namespace: configuredConn.Namespace,
			},
			Data: map[string][]byte{"kubeconfig.yaml": []byte(k3kPublishedKubeconfig)},
		},
	)
	provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
		HostClientset: clientset,
		ClusterName:   "configured-from-cli-context",
	})
	require.NoError(t, err)

	out, err := provisioner.Kubeconfig(context.Background(), "safe-namespace-qualified")
	require.NoError(t, err)

	config, err := clientcmd.Load(out)
	require.NoError(t, err)

	for _, cluster := range config.Clusters {
		assert.Equal(t, safeConn.Endpoint, cluster.Server)
	}
}

func TestKubeconfig_FallsBackToNameArgument(t *testing.T) {
	t.Parallel()

	conn := k3dprovisioner.ConnectionFor("from-arg")
	clientset := k8sfake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: conn.SecretName, Namespace: conn.Namespace},
		Data:       map[string][]byte{"kubeconfig.yaml": []byte(k3kPublishedKubeconfig)},
	})

	provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
		HostClientset: clientset,
	})
	require.NoError(t, err)

	out, err := provisioner.Kubeconfig(context.Background(), "from-arg")
	require.NoError(t, err)

	config, err := clientcmd.Load(out)
	require.NoError(t, err)

	for _, cluster := range config.Clusters {
		assert.Equal(t, conn.Endpoint, cluster.Server)
	}
}

func TestKubeconfig_ErrorsWithoutAnyName(t *testing.T) {
	t.Parallel()

	provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
		HostClientset: k8sfake.NewClientset(),
	})
	require.NoError(t, err)

	_, err = provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrConfigNil)
}

func TestKubeconfig_ErrorsWithoutHostClientset(t *testing.T) {
	t.Parallel()

	provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
		ClusterName: "nested-k3s",
	})
	require.NoError(t, err)

	_, err = provisioner.Kubeconfig(context.Background(), "")

	require.ErrorIs(t, err, clustererr.ErrConfigNil)
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

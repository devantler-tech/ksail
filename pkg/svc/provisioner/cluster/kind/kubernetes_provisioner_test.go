package kindprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

const exposedKindTestURL = "https://advertised.example:31234"

func TestFinalizeKubeconfigVerifiesExposureBeforePublishing(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		reachable bool
	}{
		{name: "unreachable", reachable: false},
		{name: "reachable", reachable: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "kubeconfig")
			require.NoError(t, os.WriteFile(path, []byte(kindHostKubeconfig), 0o600))

			clientset := k8sfake.NewClientset()
			provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
				clientset, kindConnectorTestCluster, path,
			)
			conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
			verified := false

			provisioner.WithWaitForReadyForTest(
				func(ctx context.Context, gotPath, gotContext string) error {
					verified = true

					assert.Equal(t, path, gotPath)
					assertKindExposureWait(ctx, t, gotPath, gotContext)
					assertNoKindConnector(ctx, t, clientset)

					if !testCase.reachable {
						return assert.AnError
					}

					return nil
				},
			)

			err := provisioner.FinalizeKubeconfigForTest(
				t.Context(),
				kindConnectorTestCluster,
				exposedKindTestURL,
			)
			assert.True(t, verified, "create must verify the rewritten authenticated route")

			if !testCase.reachable {
				require.ErrorIs(t, err, assert.AnError)
				assertNoKindConnector(t.Context(), t, clientset)

				return
			}

			require.NoError(t, err)
			published, err := provisioner.Kubeconfig(t.Context(), kindConnectorTestCluster)
			require.NoError(t, err)
			config, err := clientcmd.Load(published)
			require.NoError(t, err)
			assert.Equal(t, exposedKindTestURL, config.Clusters[conn.ContextName].Server)
		})
	}
}

func assertKindExposureWait(ctx context.Context, t *testing.T, path, contextName string) {
	t.Helper()

	conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
	assert.Equal(t, conn.ContextName, contextName)

	deadline, bounded := ctx.Deadline()
	assert.True(t, bounded, "exposure verification must have its own deadline")
	assert.LessOrEqual(t, time.Until(deadline), time.Minute)

	config, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, exposedKindTestURL, config.Clusters[conn.ContextName].Server)
}

func assertNoKindConnector(ctx context.Context, t *testing.T, clientset *k8sfake.Clientset) {
	t.Helper()

	conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
	_, err := clientset.CoreV1().
		Secrets(conn.Namespace).
		Get(ctx, conn.SecretName, metav1.GetOptions{})
	assert.True(t, apierrors.IsNotFound(err), "an unverified connector must not be published")
}

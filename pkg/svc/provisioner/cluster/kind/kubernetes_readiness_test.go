package kindprovisioner_test

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

func TestKubernetesFinalizePublishesOnlyAfterAdvertisedAuthenticatedReadiness(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name                         string
		readyStatus, namespaceStatus int
		wantSuccess                  bool
		untrustedTLS                 bool
	}{
		{name: "ready and authorized", readyStatus: 200, namespaceStatus: 200, wantSuccess: true},
		{name: "advertised API not ready", readyStatus: 503, namespaceStatus: 200},
		{name: "ready but unauthorized", readyStatus: 200, namespaceStatus: 403},
		{name: "untrusted TLS", readyStatus: 200, namespaceStatus: 200, untrustedTLS: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var readyCalls, authorizedCalls atomic.Int32

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			server := httptest.NewTLSServer(readinessHandler(cancel,
				testCase.readyStatus, testCase.namespaceStatus, &readyCalls, &authorizedCalls))
			t.Cleanup(server.Close)

			path := writeReadinessKubeconfig(t, server, testCase.untrustedTLS)

			client := fake.NewClientset()
			provisioner := kindprovisioner.NewKubernetesProvisionerForConnectorTest(
				client, kindConnectorTestCluster, path,
			)
			err := provisioner.FinalizeKubeconfigForTest(ctx, kindConnectorTestCluster, server.URL)
			conn := kindprovisioner.ConnectionFor(kindConnectorTestCluster)
			secret, secretErr := client.CoreV1().Secrets(conn.Namespace).
				Get(context.Background(), conn.SecretName, metav1.GetOptions{})

			if !testCase.wantSuccess {
				require.Error(t, err)
				assert.True(
					t,
					apierrors.IsNotFound(secretErr),
					"unready connector must remain unpublished",
				)

				return
			}

			require.NoError(t, err)
			require.NoError(t, secretErr)
			assert.Positive(t, readyCalls.Load())
			assert.Positive(t, authorizedCalls.Load())
			published, err := clientcmd.Load(secret.Data["kubeconfig.yaml"])
			require.NoError(t, err)
			assert.Equal(t, server.URL, published.Clusters["kind-nested-kind"].Server)
			assert.False(t, published.Clusters["kind-nested-kind"].InsecureSkipTLSVerify)
		})
	}
}

func readinessHandler(
	cancel context.CancelFunc, readyStatus, namespaceStatus int,
	readyCalls, authorizedCalls *atomic.Int32,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer nested-token" {
			writer.WriteHeader(http.StatusUnauthorized)

			return
		}

		switch request.URL.Path {
		case "/readyz":
			readyCalls.Add(1)
			writer.WriteHeader(readyStatus)
			_, _ = writer.Write([]byte("ok"))

			if readyStatus != http.StatusOK {
				cancel()
			}
		case "/api/v1/namespaces":
			authorizedCalls.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(namespaceStatus)
			_, _ = writer.Write([]byte(`{"kind":"NamespaceList","apiVersion":"v1","items":[]}`))

			if namespaceStatus != http.StatusOK {
				cancel()
			}
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	})
}

func writeReadinessKubeconfig(t *testing.T, server *httptest.Server, untrustedTLS bool) string {
	t.Helper()

	config, err := clientcmd.Load([]byte(kindHostKubeconfig))
	require.NoError(t, err)

	config.Clusters["kind-nested-kind"].CertificateAuthorityData = pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw},
	)
	if untrustedTLS {
		config.Clusters["kind-nested-kind"].CertificateAuthorityData = nil
	}

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, clientcmd.WriteToFile(*config, path))

	return path
}

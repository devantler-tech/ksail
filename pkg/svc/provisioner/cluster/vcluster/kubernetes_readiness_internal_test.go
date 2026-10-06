package vclusterprovisioner

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestMergeReadyVClusterKubeconfigDoesNotPublishUnreadyEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	path, original := readinessHostKubeconfig(t)
	config := readinessNestedKubeconfig(t, server.URL)

	err := mergeReadyVClusterKubeconfig(context.Background(), path, config, time.Millisecond, 30*time.Millisecond)
	require.Error(t, err, "creation must not publish an endpoint that never became ready")

	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, original, after, "a failed readiness check must not change the host kubeconfig")
}

func TestMergeReadyVClusterKubeconfigRetriesRewrittenAuthenticatedEndpoint(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		_, _ = w.Write([]byte("ok\n"))
	}))
	t.Cleanup(server.Close)

	path, _ := readinessHostKubeconfig(t)
	config := readinessNestedKubeconfig(t, server.URL)
	trusted, err := clientcmd.Load(config)
	require.NoError(t, err)
	trusted.Clusters["vcluster-ready-test"].CertificateAuthorityData = pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE", Bytes: server.Certificate().Raw,
	})
	config, err = clientcmd.Write(*trusted)
	require.NoError(t, err)

	err = mergeReadyVClusterKubeconfig(context.Background(), path, config, time.Millisecond, time.Second)
	require.NoError(t, err)
	require.GreaterOrEqual(t, calls.Load(), int32(2))

	saved, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	require.Equal(t, server.URL, saved.Clusters["vcluster-ready-test"].Server)
	require.Contains(t, saved.Contexts, "host", "the existing host context must survive publication")
}

func TestMergeReadyVClusterKubeconfigRejectsInvalidResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not ready"))
	}))
	t.Cleanup(server.Close)

	path, original := readinessHostKubeconfig(t)
	err := mergeReadyVClusterKubeconfig(
		context.Background(), path, readinessNestedKubeconfig(t, server.URL), time.Millisecond, 30*time.Millisecond,
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestMergeReadyVClusterKubeconfigRejectsRefusedConnection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL
	server.Close()

	path, original := readinessHostKubeconfig(t)
	err := mergeReadyVClusterKubeconfig(
		context.Background(), path, readinessNestedKubeconfig(t, endpoint), time.Millisecond, 30*time.Millisecond,
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestMergeReadyVClusterKubeconfigBoundsBlockedRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	path, original := readinessHostKubeconfig(t)
	err := mergeReadyVClusterKubeconfig(
		context.Background(), path, readinessNestedKubeconfig(t, server.URL), time.Millisecond, 30*time.Millisecond,
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestMergeReadyVClusterKubeconfigHonorsCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	path, original := readinessHostKubeconfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := mergeReadyVClusterKubeconfig(ctx, path, readinessNestedKubeconfig(t, server.URL), time.Millisecond, time.Second)
	require.ErrorIs(t, err, context.Canceled)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestMergeReadyVClusterKubeconfigChecksReadinessWithoutOutputPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	err := mergeReadyVClusterKubeconfig(
		context.Background(), "", readinessNestedKubeconfig(t, server.URL), time.Millisecond, 30*time.Millisecond,
	)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMergeReadyVClusterKubeconfigRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	path, original := readinessHostKubeconfig(t)
	err := mergeReadyVClusterKubeconfig(context.Background(), path, []byte("invalid"), time.Millisecond, time.Second)
	require.Error(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func readinessHostKubeconfig(t *testing.T) (string, []byte) {
	t.Helper()

	config := clientcmdapi.NewConfig()
	config.Clusters["host"] = &clientcmdapi.Cluster{Server: "https://host.invalid"}
	config.AuthInfos["host"] = &clientcmdapi.AuthInfo{}
	config.Contexts["host"] = &clientcmdapi.Context{Cluster: "host", AuthInfo: "host"}
	config.CurrentContext = "host"
	data, err := clientcmd.Write(*config)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path, data
}

func readinessNestedKubeconfig(t *testing.T, endpoint string) []byte {
	t.Helper()

	config := clientcmdapi.NewConfig()
	config.Clusters["original"] = &clientcmdapi.Cluster{Server: "https://secret-endpoint.invalid"}
	config.AuthInfos["original"] = &clientcmdapi.AuthInfo{Token: "fixture-token"}
	config.Contexts["original"] = &clientcmdapi.Context{Cluster: "original", AuthInfo: "original"}
	config.CurrentContext = "original"
	data, err := clientcmd.Write(*config)
	require.NoError(t, err)

	rewritten, err := rewriteVClusterKubeconfig(data, endpoint, "ready-test")
	require.NoError(t, err)

	return rewritten
}

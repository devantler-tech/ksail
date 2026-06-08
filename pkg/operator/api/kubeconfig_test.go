package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
)

// kubeconfigStub implements api.KubeconfigProvider, returning canned bytes.
type kubeconfigStub struct {
	stubClusterService
}

func (kubeconfigStub) Kubeconfig(_ context.Context, _, _ string) ([]byte, error) {
	return []byte("apiVersion: v1\nkind: Config\n"), nil
}

func TestConfigReportsKubeconfigDownload(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: kubeconfigStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"kubeconfigDownload":true`)
}

func TestKubeconfigEndpoint(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: kubeconfigStub{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/prod/kubeconfig",
		"",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/yaml", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), `prod.kubeconfig`)
	assert.Contains(t, recorder.Body.String(), "kind: Config")
}

func TestKubeconfigEndpointUnregisteredWithoutProvider(t *testing.T) {
	t.Parallel()

	// A plain ClusterService (no KubeconfigProvider) does not get the kubeconfig route, and config
	// reports kubeconfigDownload:false.
	server := &api.Server{Service: stubClusterService{}}

	config := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")
	assert.Contains(t, config.Body.String(), `"kubeconfigDownload":false`)

	kubeconfig := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/prod/kubeconfig",
		"",
	)
	assert.Equal(t, http.StatusNotFound, kubeconfig.Code)
}

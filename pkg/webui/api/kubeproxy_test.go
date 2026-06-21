package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// kubeProxyStub is a ClusterService that also implements api.KubeProxy, recording the path/query it
// received and returning a canned response so the proxy route can be exercised without an apiserver.
type kubeProxyStub struct {
	stubClusterService

	body     string
	gotPath  string
	gotQuery string
}

func (k *kubeProxyStub) ProxyKubeGet(
	_ context.Context,
	_, _, path string,
	query url.Values,
) (api.KubeProxyResponse, error) {
	k.gotPath = path
	k.gotQuery = query.Encode()

	return api.KubeProxyResponse{
		Status:      http.StatusOK,
		ContentType: "application/json",
		Body:        io.NopCloser(strings.NewReader(k.body)),
	}, nil
}

func TestKubeProxyStreamsResponseAndForwardsPathQuery(t *testing.T) {
	t.Parallel()

	stub := &kubeProxyStub{body: `{"kind":"PodList"}`}
	server := &api.Server{Service: stub}

	target := "/api/v1/clusters/default/kind/proxy/api/v1/pods?labelSelector=app%3Dx"
	recorder := doRequest(server.Handler(), http.MethodGet, target, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200", recorder.Code)
	}

	if got := recorder.Body.String(); got != `{"kind":"PodList"}` {
		t.Errorf("proxy body = %q", got)
	}

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type = %q, want application/json", got)
	}

	if stub.gotPath != "api/v1/pods" {
		t.Errorf("forwarded path = %q, want api/v1/pods", stub.gotPath)
	}

	if stub.gotQuery != "labelSelector=app%3Dx" {
		t.Errorf("forwarded query = %q, want labelSelector=app%%3Dx", stub.gotQuery)
	}
}

func TestConfigAdvertisesKubeProxyCapability(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: &kubeProxyStub{}}
	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	var config struct {
		Capabilities struct {
			KubeProxy bool `json:"kubeProxy"`
		} `json:"capabilities"`
	}

	err := json.Unmarshal(recorder.Body.Bytes(), &config)
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if !config.Capabilities.KubeProxy {
		t.Error("capabilities.kubeProxy = false, want true for a KubeProxy backend")
	}
}

func TestKubeProxyRouteUnregisteredWithoutService(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: stubClusterService{}}

	recorder := doRequest(
		server.Handler(),
		http.MethodGet,
		"/api/v1/clusters/default/kind/proxy/api/v1/pods",
		"",
	)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("proxy route status = %d, want 404 when unregistered", recorder.Code)
	}
}

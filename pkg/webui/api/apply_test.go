package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
)

// applyStub implements api.ApplyService, recording what it received.
type applyStub struct {
	stubClusterService

	gotManifests string
	gotDryRun    bool
}

func (a *applyStub) ApplyManifests(
	_ context.Context, _, _ string, manifests []byte, dryRun bool,
) ([]api.ApplyResult, error) {
	a.gotManifests = string(manifests)
	a.gotDryRun = dryRun

	return []api.ApplyResult{{Kind: "ConfigMap", Name: "cm1", Status: "applied"}}, nil
}

func TestConfigReportsApplyManifests(t *testing.T) {
	t.Parallel()

	server := &api.Server{Service: &applyStub{}}

	recorder := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"applyManifests":true`)
}

func TestApplyEndpoint(t *testing.T) {
	t.Parallel()

	stub := &applyStub{}
	server := &api.Server{Service: stub}

	recorder := doApplyRequest(
		server.Handler(),
		"application/yaml",
		"/api/v1/clusters/default/c1/apply",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "cm1")
	assert.Contains(t, recorder.Body.String(), `"dryRun":false`)
	assert.Contains(t, stub.gotManifests, "kind: ConfigMap")
	assert.False(t, stub.gotDryRun)
}

func TestApplyEndpointDryRun(t *testing.T) {
	t.Parallel()

	stub := &applyStub{}
	server := &api.Server{Service: stub}

	recorder := doApplyRequest(
		server.Handler(),
		"application/yaml",
		"/api/v1/clusters/default/c1/apply?dryRun=true",
	)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.True(t, stub.gotDryRun)
}

func TestApplyBlockedWhenReadOnly(t *testing.T) {
	t.Parallel()

	stub := &applyStub{}
	server := &api.Server{Service: stub, ReadOnly: true}

	recorder := doApplyRequest(
		server.Handler(),
		"application/yaml",
		"/api/v1/clusters/default/c1/apply",
	)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, stub.gotManifests)
}

func TestApplyRejectsCORSSafelistedContentType(t *testing.T) {
	t.Parallel()

	stub := &applyStub{}
	server := &api.Server{Service: stub}

	recorder := doApplyRequest(
		server.Handler(),
		"text/plain",
		"/api/v1/clusters/default/c1/apply",
	)

	assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
	assert.Empty(t, stub.gotManifests)
	assert.Contains(t, recorder.Body.String(), "application/yaml")
}

func TestApplyRejectsHostileMatchingHostAndOrigin(t *testing.T) {
	t.Parallel()

	stub := &applyStub{}
	server := &api.Server{
		Service:  stub,
		UIOrigin: "http://127.0.0.1:8080",
	}

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"/api/v1/clusters/default/c1/apply",
		strings.NewReader("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n"),
	)
	request.Host = "attacker.example"
	request.Header.Set("Content-Type", "application/yaml")
	request.Header.Set("Origin", "http://attacker.example")

	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, stub.gotManifests)
}

func TestApplyAllowsConfiguredUIOrigin(t *testing.T) {
	t.Parallel()

	stub := &applyStub{}
	server := &api.Server{
		Service:  stub,
		UIOrigin: "http://127.0.0.1:8080",
	}

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://127.0.0.1:8080/api/v1/clusters/default/c1/apply",
		strings.NewReader("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n"),
	)
	request.Header.Set("Content-Type", "application/yaml")
	request.Header.Set("Origin", "http://127.0.0.1:8080")

	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, stub.gotManifests, "kind: ConfigMap")
}

func TestApplyAllowsDefaultPortEquivalentUIOrigin(t *testing.T) {
	t.Parallel()

	stub := &applyStub{}
	server := &api.Server{
		Service:  stub,
		UIOrigin: "http://127.0.0.1:80",
	}

	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://127.0.0.1/api/v1/clusters/default/c1/apply",
		strings.NewReader("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n"),
	)
	request.Header.Set("Content-Type", "application/yaml")
	request.Header.Set("Origin", "http://127.0.0.1")

	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, stub.gotManifests, "kind: ConfigMap")
}

func doApplyRequest(
	handler http.Handler,
	contentType string,
	target string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		target,
		strings.NewReader("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n"),
	)
	request.Header.Set("Content-Type", contentType)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder
}

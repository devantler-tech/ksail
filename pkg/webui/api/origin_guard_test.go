package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
)

const (
	guardUIOrigin  = "http://127.0.0.1:8080"
	guardUIHost    = "127.0.0.1:8080"
	rebindHost     = "rebind.attacker.example:8080"
	rebindOrigin   = "http://rebind.attacker.example:8080"
	guardProxyPath = "/api/v1/clusters/default/dev/proxy/api/v1/namespaces/default/secrets"
)

// guardedRequest sends a GET request with the given Host and Origin to a local-mode server whose UI origin
// is guardUIOrigin, and returns the recorder.
func guardedRequest(
	t *testing.T,
	service api.ClusterService,
	target, host, origin string,
) *httptest.ResponseRecorder {
	t.Helper()

	server := &api.Server{Service: service, UIOrigin: guardUIOrigin}

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	request.Host = host

	if origin != "" {
		request.Header.Set("Origin", origin)
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	return recorder
}

// TestOriginGuardRejectsRebindingReads pins the DNS rebinding defence for reads. After an attacker's
// hostname is rebound to 127.0.0.1, the browser sends that hostname as Host, so every route, including
// the GET routes that read cluster data with the user's kubeconfig, must refuse a Host that is not the
// configured UI origin.
func TestOriginGuardRejectsRebindingReads(t *testing.T) {
	t.Parallel()

	targets := []string{
		guardProxyPath,
		"/api/v1/clusters/default/dev/watch/api/v1/namespaces/default/secrets",
		"/wsMultiplexer",
		"/api/v1/clusters",
		"/api/v1/settings",
		"/",
	}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			stub := &kubeProxyStub{body: `{"kind":"SecretList"}`}
			recorder := guardedRequest(t, stub, target, rebindHost, rebindOrigin)

			assert.Equal(t, http.StatusForbidden, recorder.Code)
			assert.Empty(t, stub.gotPath, "the cluster proxy must not be reached")
			assert.NotContains(t, recorder.Body.String(), "SecretList")
		})
	}
}

// TestOriginGuardRejectsRebindingReadWithoutOrigin covers a same-origin GET after rebinding, which a
// browser may send without an Origin header: the Host alone must be enough to refuse it.
func TestOriginGuardRejectsRebindingReadWithoutOrigin(t *testing.T) {
	t.Parallel()

	stub := &kubeProxyStub{body: `{"kind":"SecretList"}`}
	recorder := guardedRequest(t, stub, guardProxyPath, rebindHost, "")

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, stub.gotPath)
}

// TestOriginGuardRejectsReadFromForeignOrigin covers a request that addresses the right Host but comes
// from a page on another origin.
func TestOriginGuardRejectsReadFromForeignOrigin(t *testing.T) {
	t.Parallel()

	stub := &kubeProxyStub{body: `{"kind":"SecretList"}`}
	recorder := guardedRequest(t, stub, guardProxyPath, guardUIHost, rebindOrigin)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Empty(t, stub.gotPath)
}

// TestOriginGuardAllowsReadsFromTheUIOrigin pins the everyday path: the SPA served from the listener,
// and a non-browser client that omits Origin, still read through the proxy.
func TestOriginGuardAllowsReadsFromTheUIOrigin(t *testing.T) {
	t.Parallel()

	for _, origin := range []string{guardUIOrigin, ""} {
		t.Run("origin="+origin, func(t *testing.T) {
			t.Parallel()

			stub := &kubeProxyStub{body: `{"kind":"SecretList"}`}
			recorder := guardedRequest(t, stub, guardProxyPath, guardUIHost, origin)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, "api/v1/namespaces/default/secrets", stub.gotPath)
		})
	}
}

// TestOriginGuardIsOffWithoutUIOrigin pins the modes that own their origin boundary elsewhere (the
// desktop webview and the in-cluster operator): with no UIOrigin, reads are not filtered by Host.
func TestOriginGuardIsOffWithoutUIOrigin(t *testing.T) {
	t.Parallel()

	stub := &kubeProxyStub{body: `{"kind":"PodList"}`}
	server := &api.Server{Service: stub}

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/clusters/default/dev/proxy/api/v1/pods", nil)
	request.Host = rebindHost

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "api/v1/pods", stub.gotPath)
}

package api_test

import (
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const indexHTML = "<!doctype html><title>KSail</title>"

func uiFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte(indexHTML)},
		"assets/app-abc123.js": {Data: []byte("console.log('ksail')")},
	}
}

func serverWithUI(t *testing.T) *api.Server {
	t.Helper()

	return &api.Server{Client: newClient(t), UIFS: uiFS()}
}

func TestServesSPAIndexAtRoot(t *testing.T) {
	t.Parallel()

	recorder := doRequest(serverWithUI(t).Handler(), http.MethodGet, "/", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, indexHTML, recorder.Body.String())
	assert.Equal(t, "no-cache", recorder.Header().Get("Cache-Control"))
}

func TestServesHashedAssetWithImmutableCache(t *testing.T) {
	t.Parallel()

	recorder := doRequest(serverWithUI(t).Handler(), http.MethodGet, "/assets/app-abc123.js", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", recorder.Header().Get("Cache-Control"))
}

func TestSPAFallbackServesIndexForUnknownPath(t *testing.T) {
	t.Parallel()

	// An unknown (client-routed) path must return the SPA entry point, not 404.
	recorder := doRequest(serverWithUI(t).Handler(), http.MethodGet, "/clusters/foo", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, indexHTML, recorder.Body.String())
}

func TestAPIRoutesNotShadowedByUI(t *testing.T) {
	t.Parallel()

	// With a UI mounted at "/", API paths must still hit the API and return JSON.
	recorder := doRequest(serverWithUI(t).Handler(), http.MethodGet, "/api/v1/config", "")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
}

func TestNoUIServingWhenUIFSNil(t *testing.T) {
	t.Parallel()

	// Without an embedded UI, the root path has no handler and returns 404; the API still works.
	server := &api.Server{Client: newClient(t)}

	root := doRequest(server.Handler(), http.MethodGet, "/", "")
	assert.Equal(t, http.StatusNotFound, root.Code)

	config := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")
	assert.Equal(t, http.StatusOK, config.Code)
}

func TestSecurityHeadersApplied(t *testing.T) {
	t.Parallel()

	// Security headers wrap every response, including the API when no UI is present.
	recorder := doRequest((&api.Server{Client: newClient(t)}).Handler(), http.MethodGet, "/api/v1/config", "")

	header := recorder.Header()
	assert.Equal(t, "nosniff", header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", header.Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", header.Get("Referrer-Policy"))
	require.NotEmpty(t, header.Get("Content-Security-Policy"))
	assert.Contains(t, header.Get("Content-Security-Policy"), "default-src 'self'")
}

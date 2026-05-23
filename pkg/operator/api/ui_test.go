package api_test

import (
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoUIServingWhenStaticFSNil(t *testing.T) {
	t.Parallel()

	// Without an embedded UI, the root path has no handler and returns 404; the API still works.
	server := &api.Server{Service: api.NewCRClusterService(newClient(t))}

	root := doRequest(server.Handler(), http.MethodGet, "/", "")
	assert.Equal(t, http.StatusNotFound, root.Code)

	config := doRequest(server.Handler(), http.MethodGet, "/api/v1/config", "")
	assert.Equal(t, http.StatusOK, config.Code)
}

func TestSecurityHeadersApplied(t *testing.T) {
	t.Parallel()

	// Security headers wrap every response, including the API when no UI is present.
	recorder := doRequest(
		(&api.Server{Service: api.NewCRClusterService(newClient(t))}).Handler(),
		http.MethodGet,
		"/api/v1/config",
		"",
	)

	header := recorder.Header()
	assert.Equal(t, "nosniff", header.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", header.Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", header.Get("Referrer-Policy"))
	require.NotEmpty(t, header.Get("Content-Security-Policy"))
	assert.Contains(t, header.Get("Content-Security-Policy"), "default-src 'self'")
}

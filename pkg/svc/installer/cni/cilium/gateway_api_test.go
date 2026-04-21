package ciliuminstaller_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleCRDBundle = `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gateways.gateway.networking.k8s.io
spec:
  group: gateway.networking.k8s.io
  names:
    kind: Gateway
    plural: gateways
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: httproutes.gateway.networking.k8s.io
spec:
  group: gateway.networking.k8s.io
  names:
    kind: HTTPRoute
    plural: httproutes
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`

const sampleMixedBundle = `---
apiVersion: v1
kind: Namespace
metadata:
  name: gateway-system
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gateways.gateway.networking.k8s.io
spec:
  group: gateway.networking.k8s.io
  names:
    kind: Gateway
    plural: gateways
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`

func TestParseGatewayAPICRDs(t *testing.T) {
	t.Parallel()

	t.Run("parses multiple CRDs from bundle", func(t *testing.T) {
		t.Parallel()

		crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(sampleCRDBundle))

		require.NoError(t, err)
		require.Len(t, crds, 2)
		assert.Equal(t, "gateways.gateway.networking.k8s.io", crds[0].Name)
		assert.Equal(t, "httproutes.gateway.networking.k8s.io", crds[1].Name)
	})

	t.Run("returns empty slice for empty input", func(t *testing.T) {
		t.Parallel()

		crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(""))

		require.NoError(t, err)
		assert.Empty(t, crds)
	})

	t.Run("skips non-CRD documents", func(t *testing.T) {
		t.Parallel()

		crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(sampleMixedBundle))

		require.NoError(t, err)
		require.Len(t, crds, 1)
		assert.Equal(t, "gateways.gateway.networking.k8s.io", crds[0].Name)
	})

	t.Run("handles whitespace-only documents", func(t *testing.T) {
		t.Parallel()

		crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte("---\n\n---\n  \n"))

		require.NoError(t, err)
		assert.Empty(t, crds)
	})
}

func TestFetchGatewayAPICRDs(t *testing.T) {
	t.Parallel()

	t.Run("fetches and parses CRDs from server", func(t *testing.T) {
		t.Parallel()

		server := newCRDServer(http.StatusOK, sampleCRDBundle)
		defer server.Close()

		crds, err := ciliuminstaller.FetchGatewayAPICRDs(
			context.Background(), server.URL, 5*time.Second,
		)

		require.NoError(t, err)
		require.Len(t, crds, 2)
		assert.Equal(t, "gateways.gateway.networking.k8s.io", crds[0].Name)
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		t.Parallel()

		server := newCRDServer(http.StatusNotFound, "")
		defer server.Close()

		_, err := ciliuminstaller.FetchGatewayAPICRDs(
			context.Background(), server.URL, 5*time.Second,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected")
		assert.Contains(t, err.Error(), "404")
		assert.Contains(t, err.Error(), server.URL)
	})

	t.Run("returns error on connection failure", func(t *testing.T) {
		t.Parallel()

		_, err := ciliuminstaller.FetchGatewayAPICRDs(
			context.Background(), "http://127.0.0.1:0/nonexistent", 5*time.Second,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "download CRDs")
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()

		server := newCRDServer(http.StatusOK, sampleCRDBundle)
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := ciliuminstaller.FetchGatewayAPICRDs(
			ctx, server.URL, 5*time.Second,
		)

		require.Error(t, err)
	})
}

func newCRDServer(statusCode int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if statusCode != http.StatusOK {
			writer.WriteHeader(statusCode)

			return
		}

		writer.Header().Set("Content-Type", "application/x-yaml")
		_, _ = writer.Write([]byte(body))
	}))
}

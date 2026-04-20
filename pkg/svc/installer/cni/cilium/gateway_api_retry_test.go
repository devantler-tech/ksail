package ciliuminstaller_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const retryTestCRDBundle = `---
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

func TestFetchGatewayAPICRDsWithRetry_RetriesTransientHTTPStatus(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			attempt := attempts.Add(1)
			if attempt < 3 {
				writer.WriteHeader(http.StatusGatewayTimeout)

				return
			}

			writer.Header().Set("Content-Type", "application/x-yaml")
			_, _ = writer.Write([]byte(retryTestCRDBundle))
		}),
	)
	defer server.Close()

	crds, err := ciliuminstaller.FetchGatewayAPICRDsWithRetryForTest(
		context.Background(),
		server.URL,
		time.Second,
		3,
		time.Millisecond,
		2*time.Millisecond,
	)

	require.NoError(t, err)
	require.Len(t, crds, 1)
	assert.EqualValues(t, 3, attempts.Load())
}

func TestFetchGatewayAPICRDsWithRetry_DoesNotRetryNonRetryableStatus(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			writer.WriteHeader(http.StatusNotFound)
		}),
	)
	defer server.Close()

	_, err := ciliuminstaller.FetchGatewayAPICRDsWithRetryForTest(
		context.Background(),
		server.URL,
		time.Second,
		3,
		time.Millisecond,
		2*time.Millisecond,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.EqualValues(t, 1, attempts.Load())
}

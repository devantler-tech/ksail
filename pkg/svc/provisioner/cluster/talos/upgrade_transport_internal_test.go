package talosprovisioner

import (
	"context"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/siderolabs/go-kubernetes/kubernetes/ssa"
	"github.com/siderolabs/talos/pkg/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type upgradeRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip upgradeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestKubernetesUpgradeReadTransportDoesNotReplayOtherRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		body   string
		err    error
	}{
		{name: "create", method: http.MethodPost, err: refusedUpgradeConnection()},
		{name: "update", method: http.MethodPut, err: refusedUpgradeConnection()},
		{name: "apply", method: http.MethodPatch, err: refusedUpgradeConnection()},
		{name: "delete", method: http.MethodDelete, err: refusedUpgradeConnection()},
		{
			name:   "read with body",
			method: http.MethodGet,
			body:   "body",
			err:    refusedUpgradeConnection(),
		},
		{name: "permanent error", method: http.MethodGet, err: syscall.EACCES},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			transport := upgradeReadTransport{transport: upgradeRoundTripper(
				func(*http.Request) (*http.Response, error) {
					calls++

					return nil, test.err
				})}
			request, err := http.NewRequestWithContext(t.Context(), test.method,
				"https://upgrade.invalid", strings.NewReader(test.body))
			require.NoError(t, err)

			response, err := transport.RoundTrip(request)
			if response != nil {
				require.NoError(t, response.Body.Close())
			}

			require.ErrorIs(t, err, test.err)
			assert.Nil(t, response)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestKubernetesUpgradeReadTransportPreservesHTTPFailure(t *testing.T) {
	t.Parallel()

	calls := 0
	transport := upgradeReadTransport{transport: upgradeRoundTripper(
		func(request *http.Request) (*http.Response, error) {
			calls++
			response := upgradeJSONResponse(request, "forbidden")
			response.StatusCode = http.StatusForbidden

			return response, nil
		})}
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"https://upgrade.invalid",
		nil,
	)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)
	require.NoError(t, err)

	defer response.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	assert.Equal(t, 1, calls)
}

func TestKubernetesUpgradeReadTransportStopsAtRetryLimit(t *testing.T) {
	t.Parallel()

	calls := 0
	transport := upgradeReadTransport{transport: upgradeRoundTripper(
		func(*http.Request) (*http.Response, error) {
			calls++

			return nil, refusedUpgradeConnection()
		})}
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"https://upgrade.invalid",
		nil,
	)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}

	require.ErrorIs(t, err, refusedUpgradeErrno())
	assert.Nil(t, response)
	assert.Equal(t, 6, calls)
}

func TestKubernetesUpgradeReadTransportStopsOnCancellation(t *testing.T) {
	t.Parallel()

	for _, beforeRequest := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "during backoff", true: "before request"}[beforeRequest],
			func(t *testing.T) {
				t.Parallel()

				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				if beforeRequest {
					cancel()
				}

				calls := 0
				transport := upgradeReadTransport{transport: upgradeRoundTripper(
					func(*http.Request) (*http.Response, error) {
						calls++

						cancel()

						return nil, refusedUpgradeConnection()
					})}
				request, err := http.NewRequestWithContext(
					ctx,
					http.MethodGet,
					"https://upgrade.invalid",
					nil,
				)
				require.NoError(t, err)

				started := time.Now()

				response, err := transport.RoundTrip(request)
				if response != nil {
					require.NoError(t, response.Body.Close())
				}

				require.ErrorIs(t, err, context.Canceled)
				assert.Nil(t, response)
				assert.Less(t, time.Since(started), time.Second)

				if beforeRequest {
					assert.Zero(t, calls)
				} else {
					assert.Equal(t, 1, calls)
				}
			},
		)
	}
}

func TestKubernetesUpgradeProviderPreservesOriginalTransport(t *testing.T) {
	t.Parallel()

	calls := 0
	config := &rest.Config{
		Host: "https://upgrade.invalid",
		Transport: upgradeRoundTripper(func(request *http.Request) (*http.Response, error) {
			calls++

			assert.Equal(t, "preserved", request.Header.Get("X-Upgrade-Test"))

			return nil, refusedUpgradeConnection()
		}),
		WrapTransport: func(transport http.RoundTripper) http.RoundTripper {
			return upgradeRoundTripper(func(request *http.Request) (*http.Response, error) {
				request = request.Clone(request.Context())
				request.Header.Set("X-Upgrade-Test", "preserved")

				return transport.RoundTrip(request)
			})
		},
	}
	provider := kubernetesUpgradeProvider(upgradeConfigProvider{config: config})
	upgradeConfig, err := provider.K8sRestConfig(t.Context())
	require.NoError(t, err)
	require.NotSame(t, config, upgradeConfig)

	// The source config must retain its single-attempt behavior.
	client, err := rest.HTTPClientFor(config)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, config.Host, nil)
	require.NoError(t, err)

	response, err := client.Do(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}

	require.ErrorIs(t, err, refusedUpgradeErrno())
	assert.Equal(t, 1, calls)

	// The decorated config must still invoke the original wrapper.
	client, err = rest.HTTPClientFor(upgradeConfig)
	require.NoError(t, err)

	request.Method = http.MethodPatch

	response, err = client.Do(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}

	require.ErrorIs(t, err, refusedUpgradeErrno())
	assert.Equal(t, 2, calls)
}

type upgradeConfigProvider struct {
	cluster.K8sProvider

	config *rest.Config
}

func (provider upgradeConfigProvider) K8sRestConfig(context.Context) (*rest.Config, error) {
	return provider.config, nil
}

func refusedUpgradeConnection() error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: refusedUpgradeErrno()}
}

func refusedUpgradeErrno() syscall.Errno {
	if runtime.GOOS == "windows" {
		// Real Winsock connection refusal. syscall.ECONNREFUSED is synthetic on Windows.
		const winsockConnectionRefused = 10061

		return syscall.Errno(winsockConnectionRefused)
	}

	return syscall.ECONNREFUSED
}

func upgradeJSONResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestKubernetesUpgradeInventoryRecoversAfterConnectionRefused(t *testing.T) {
	t.Parallel()

	inventoryReads := 0
	config := &rest.Config{
		Host: "https://upgrade.invalid",
		Transport: upgradeRoundTripper(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodGet, request.Method)

			if request.URL.Path == "/api/v1/namespaces/kube-system" {
				return upgradeJSONResponse(request,
					`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"kube-system"}}`), nil
			}

			require.Equal(
				t,
				"/api/v1/namespaces/kube-system/configmaps/talos-manifests",
				request.URL.Path,
			)

			inventoryReads++

			if inventoryReads == 1 {
				return nil, refusedUpgradeConnection()
			}

			return upgradeJSONResponse(
				request,
				`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"talos-manifests","namespace":"kube-system"}}`,
			), nil
		}),
	}
	provider := kubernetesUpgradeProvider(upgradeConfigProvider{config: config})
	upgradeConfig, err := provider.K8sRestConfig(t.Context())
	require.NoError(t, err)

	client, err := kubernetes.NewForConfig(upgradeConfig)
	require.NoError(t, err)

	inventory, err := ssa.GetInventory(t.Context(), client, "kube-system", "talos-manifests")
	require.NoError(t, err)
	assert.Equal(t, "talos-manifests", inventory.ID())
	assert.Empty(t, inventory.Get())
	assert.Equal(t, 2, inventoryReads)
}

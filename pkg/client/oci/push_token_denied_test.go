package oci_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deniedAt builds the error go-containerregistry returns when the registry
// answers DENIED for the given request URL, wrapped the way the push path
// wraps it.
func deniedAt(t *testing.T, rawURL string) error {
	t.Helper()

	u, err := url.Parse(rawURL)
	require.NoError(t, err)

	return fmt.Errorf("push: %w", &transport.Error{
		StatusCode: http.StatusForbidden,
		Request:    &http.Request{Method: http.MethodGet, URL: u},
		Errors: []transport.Diagnostic{
			{Code: transport.DeniedErrorCode, Message: "denied"},
		},
	})
}

const (
	signInExchangeURL = "https://ghcr.io/token?scope=repository:org/repo:push,pull&service=ghcr.io"
	manifestPutURL    = "https://ghcr.io/v2/org/repo/manifests/latest"
)

// A DENIED answer from the registry's token exchange was measured to be
// transient (devantler-tech/platform#2957): the identical push succeeded
// minutes later. It is retried within the normal attempt budget.
func TestPushWithRetry_RetriesTokenExchangeDenial(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{deniedAt(t, signInExchangeURL), nil})

	ref, err := name.ParseReference("ghcr.io/org/repo:latest")
	require.NoError(t, err)

	err = oci.PushWithRetry(context.Background(), ref, nil, nil, push)

	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load())
}

// A denial that persists still fails, after the attempt budget, carrying the
// registry's own error.
func TestPushWithRetry_PersistentTokenExchangeDenialExhaustsAttempts(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{deniedAt(t, signInExchangeURL)})

	ref, err := name.ParseReference("ghcr.io/org/repo:latest")
	require.NoError(t, err)

	err = oci.PushWithRetry(context.Background(), ref, nil, nil, push)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "push failed after 3 attempts")
	assert.Contains(t, err.Error(), "DENIED")
	assert.Equal(t, int32(3), callCount.Load())
}

// Negative control: a DENIED from the registry API itself (not the token
// exchange) is a real authorization answer and stays non-retryable.
func TestPushWithRetry_RegistryAPIDenialStaysNonRetryable(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	push := mockPushFn(&callCount, []error{deniedAt(t, manifestPutURL)})

	ref, err := name.ParseReference("ghcr.io/org/repo:latest")
	require.NoError(t, err)

	err = oci.PushWithRetry(context.Background(), ref, nil, nil, push)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "push failed (non-retryable)")
	assert.Equal(t, int32(1), callCount.Load())
}

// flakyTokenRegistry serves an in-memory registry behind bearer auth whose
// token endpoint answers DENIED for the first deniedTokenCalls exchanges.
func flakyTokenRegistry(t *testing.T, deniedTokenCalls int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var tokenCalls atomic.Int32

	backend := registry.New()

	var server *httptest.Server

	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			if tokenCalls.Add(1) <= deniedTokenCalls {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusForbidden)
				_, _ = writer.Write([]byte(`{"errors":[{"code":"DENIED","message":"denied"}]}`))

				return
			}

			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"token":"granted"}`))

			return
		}

		if request.Header.Get("Authorization") != "Bearer granted" {
			writer.Header().Set("WWW-Authenticate",
				`Bearer realm="`+server.URL+`/token",service="test-registry"`)
			writer.WriteHeader(http.StatusUnauthorized)

			return
		}

		backend.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)

	return server, &tokenCalls
}

// End to end through go-containerregistry: the error its bearer transport
// returns for a denied token exchange is the shape the classifier matches, so
// a one-off denial no longer fails the push.
func TestPushWithRetry_RealTokenExchangeDenialIsRetried(t *testing.T) {
	t.Parallel()

	server, tokenCalls := flakyTokenRegistry(t, 1)

	ref, err := name.ParseReference(strings.TrimPrefix(server.URL, "http://")+"/org/repo:latest", name.Insecure)
	require.NoError(t, err)

	img, err := random.Image(64, 1)
	require.NoError(t, err)

	opts := []remote.Option{remote.WithAuth(&authn.Basic{Username: "user", Password: "pass"})}

	err = oci.PushWithRetry(context.Background(), ref, img, opts, remote.Write)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, tokenCalls.Load(), int32(2))

	_, err = remote.Head(ref, opts...)
	require.NoError(t, err, "the pushed image must be readable afterwards")
}

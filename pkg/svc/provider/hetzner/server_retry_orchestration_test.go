package hetzner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the server-creation retry/fallback ORCHESTRATION
// (CreateServerWithRetry and attemptServerCreationInLocation) rather than the
// pure predicate helpers covered by server_retry_test.go. They drive the
// orchestration through a mock Hetzner API (httptest) so the location-fallback,
// placement-group-fallback, retry-with-backoff and permanent-error branches are
// covered without a live cluster.
//
// Error-code choice matters: hcloud's own client retries internally only on
// conflict / rate_limit_exceeded / timeout, so those codes would be consumed by
// the client before the ksail layer ever sees them. The ksail retry path is
// therefore driven with "locked" — retryable at the ksail layer
// (IsRetryableHetznerError) but NOT retried by the hcloud client — so each
// orchestration attempt maps to exactly one mock request.

const (
	// retryTestImageID is a placeholder image ID so buildServerCreateOpts takes
	// the image (non-ISO) path, keeping the mock to a single POST /servers route.
	retryTestImageID = 12345
	// retryTestNodeName is the server name used across orchestration tests.
	retryTestNodeName = "node"
	// retryTestFallbackLocation is the secondary location used for fallback tests.
	retryTestFallbackLocation = "nbg1"
	// createdServerID is the server ID returned by the mock create endpoint.
	createdServerID = 123
)

// retryTestOpts returns baseline server-create options targeting the primary
// location via the image path; callers override individual fields as needed.
func retryTestOpts() hetzner.CreateServerOpts {
	return hetzner.CreateServerOpts{
		Name:       retryTestNodeName,
		ServerType: retryTestServerType,
		Location:   retryTestLocation,
		ImageID:    retryTestImageID,
	}
}

// writeHcloudError writes a Hetzner API error response with the given code so
// the hcloud client surfaces an hcloud.Error{Code: code}.
func writeHcloudError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(
		`{"error":{"code":"` + code + `","message":"test ` + code + `","details":null}}`,
	))
}

// newRetryTestProvider starts an httptest server with the given handler and
// returns a Provider whose hcloud client targets it.
func newRetryTestProvider(t *testing.T, handler http.Handler) *hetzner.Provider {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return hetzner.NewProvider(newTestHcloudClient(srv.URL))
}

// decodeServerCreateBody parses a POST /servers request body into a generic map.
func decodeServerCreateBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()

	var body map[string]any

	require.NoError(t, json.NewDecoder(request.Body).Decode(&body))

	return body
}

func TestCreateServerWithRetry_NilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		retryTestOpts(),
		hetzner.ServerRetryOpts{},
	)

	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, server)
}

func TestCreateServerWithRetry_NoLocations(t *testing.T) {
	t.Parallel()

	// Non-nil client so the nil-client guard passes, but no location is
	// configured (empty primary, no fallbacks) so buildLocationList is empty.
	prov := newRetryTestProvider(t, http.HandlerFunc(
		func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("no HTTP request expected when no locations are configured")
		},
	))

	opts := retryTestOpts()
	opts.Location = ""

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		opts,
		hetzner.ServerRetryOpts{},
	)

	require.ErrorIs(t, err, hetzner.ErrNoLocationsConfigured)
	assert.Nil(t, server)
}

func TestCreateServerWithRetry_SuccessFirstAttempt(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /servers", func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSONResponse(t, writer, serverCreateResp(createdServerID, 1))
	})

	prov := newRetryTestProvider(t, mux)

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		retryTestOpts(),
		hetzner.ServerRetryOpts{},
	)

	require.NoError(t, err)
	require.NotNil(t, server)
	assert.Equal(t, int64(createdServerID), server.ID)
	assert.Equal(
		t,
		int32(1),
		calls.Load(),
		"a successful first attempt must make exactly one request",
	)
}

func TestCreateServerWithRetry_PlacementFallbackSucceeds(t *testing.T) {
	t.Parallel()

	var (
		calls          atomic.Int32
		secondHadGroup atomic.Bool
	)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /servers", func(writer http.ResponseWriter, request *http.Request) {
		count := calls.Add(1)
		body := decodeServerCreateBody(t, request)

		if count == 1 {
			// First attempt carries the placement group and fails placement.
			writeHcloudError(writer, http.StatusUnprocessableEntity, "placement_error")

			return
		}

		_, hasGroup := body["placement_group"]
		secondHadGroup.Store(hasGroup)
		writeJSONResponse(t, writer, serverCreateResp(createdServerID, 1))
	})

	prov := newRetryTestProvider(t, mux)

	opts := retryTestOpts()
	opts.PlacementGroupID = 555

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		opts,
		hetzner.ServerRetryOpts{AllowPlacementFallback: true},
	)

	require.NoError(t, err)
	require.NotNil(t, server)
	assert.Equal(
		t,
		int32(2),
		calls.Load(),
		"placement fallback should retry once without the group",
	)
	assert.False(
		t,
		secondHadGroup.Load(),
		"retry after placement failure must drop the placement group",
	)
}

func TestCreateServerWithRetry_LocationFallbackSucceeds(t *testing.T) {
	t.Parallel()

	var (
		primaryCalls  atomic.Int32
		fallbackCalls atomic.Int32
	)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /servers", func(writer http.ResponseWriter, request *http.Request) {
		body := decodeServerCreateBody(t, request)
		location, _ := body["location"].(string)

		if location == retryTestLocation {
			primaryCalls.Add(1)
			// Non-retryable, non-placement error: abandon this location.
			writeHcloudError(writer, http.StatusNotFound, "not_found")

			return
		}

		fallbackCalls.Add(1)
		writeJSONResponse(t, writer, serverCreateResp(createdServerID, 1))
	})

	prov := newRetryTestProvider(t, mux)

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		retryTestOpts(),
		hetzner.ServerRetryOpts{FallbackLocations: []string{retryTestFallbackLocation}},
	)

	require.NoError(t, err)
	require.NotNil(t, server)
	assert.Equal(
		t,
		int32(1),
		primaryCalls.Load(),
		"non-retryable error must not retry the primary location",
	)
	assert.Equal(
		t,
		int32(1),
		fallbackCalls.Load(),
		"fallback location should be tried after the primary fails",
	)
}

func TestCreateServerWithRetry_AllLocationsFail(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /servers", func(writer http.ResponseWriter, _ *http.Request) {
		writeHcloudError(writer, http.StatusNotFound, "not_found")
	})

	prov := newRetryTestProvider(t, mux)

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		retryTestOpts(),
		hetzner.ServerRetryOpts{FallbackLocations: []string{retryTestFallbackLocation}},
	)

	require.ErrorIs(t, err, hetzner.ErrAllLocationsFailed)
	assert.Nil(t, server)
}

func TestCreateServerWithRetry_PermanentErrorNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /servers", func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeHcloudError(writer, http.StatusForbidden, "resource_limit_exceeded")
	})

	prov := newRetryTestProvider(t, mux)

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		retryTestOpts(),
		hetzner.ServerRetryOpts{},
	)

	require.ErrorIs(t, err, hetzner.ErrAllLocationsFailed)
	assert.Nil(t, server)
	assert.Equal(t, int32(1), calls.Load(), "a permanent resource-limit error must not be retried")
}

func TestCreateServerWithRetry_RetryThenSuccess(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /servers", func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			// "locked" is retryable at the ksail layer but not retried by the
			// hcloud client, so the ksail backoff loop drives the second attempt.
			writeHcloudError(writer, http.StatusLocked, "locked")

			return
		}

		writeJSONResponse(t, writer, serverCreateResp(createdServerID, 1))
	})

	prov := newRetryTestProvider(t, mux)

	server, err := prov.CreateServerWithRetry(
		context.Background(),
		retryTestOpts(),
		hetzner.ServerRetryOpts{},
	)

	require.NoError(t, err)
	require.NotNil(t, server)
	assert.Equal(
		t,
		int32(2),
		calls.Load(),
		"a transient error should be retried within the same location",
	)
}

func TestCreateServerWithRetry_ContextCancelledDuringRetry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var calls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /servers", func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		// Cancel before returning the transient error so the backoff wait
		// observes a cancelled context instead of sleeping for the full delay.
		cancel()
		writeHcloudError(writer, http.StatusLocked, "locked")
	})

	prov := newRetryTestProvider(t, mux)

	server, err := prov.CreateServerWithRetry(
		ctx,
		retryTestOpts(),
		hetzner.ServerRetryOpts{},
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, server)
	assert.Equal(t, int32(1), calls.Load(), "a cancelled context must stop further retry attempts")
}

package talosprovisioner_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterSchematicReturnsTheStoredID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)

				return
			}

			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).
				Encode(map[string]string{"id": "stored-id", "schematic": string(body)})
		}),
	)
	t.Cleanup(server.Close)

	id, err := talosprovisioner.RegisterSchematicForTest(
		context.Background(), server.URL, 5*time.Second, talosconfigmanager.Schematic{},
	)

	require.NoError(t, err)
	assert.Equal(t, "stored-id", id)
}

// A context with no deadline must still not wait on a stalled Image Factory forever.
func TestRegisterSchematicTimesOutOnAStalledFactory(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			select {
			case <-release:
			case <-request.Context().Done():
			}
		}),
	)

	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	start := time.Now()
	_, err := talosprovisioner.RegisterSchematicForTest(
		context.Background(), server.URL, 100*time.Millisecond, talosconfigmanager.Schematic{},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}

// An earlier cancellation on the caller's context still wins over the registration timeout.
func TestRegisterSchematicHonoursCallerCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := talosprovisioner.RegisterSchematicForTest(
		ctx, "http://127.0.0.1:1", time.Minute, talosconfigmanager.Schematic{},
	)

	require.ErrorIs(t, err, context.Canceled)
}

func TestRegisterSchematicBoundsTransientRetries(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		status      int
		failures    int32
		wantCalls   int32
		wantSuccess bool
	}{
		{"unavailable then success", http.StatusServiceUnavailable, 2, 3, true},
		{"rate limited then success", http.StatusTooManyRequests, 1, 2, true},
		{"persistent unavailable", http.StatusServiceUnavailable, 10, 3, false},
		{"invalid schematic mentioning 503", http.StatusBadRequest, 10, 1, false},
		{"missing endpoint mentioning 503", http.StatusNotFound, 10, 1, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			computed := talosconfigmanager.Schematic{}
			wantBody, err := computed.Marshal()
			require.NoError(t, err)

			var calls atomic.Int32
			server := httptest.NewServer(
				http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					assert.Equal(t, http.MethodPost, request.Method)
					assert.Equal(t, "/schematics", request.URL.Path)
					assert.Equal(t, "application/yaml", request.Header.Get("Content-Type"))
					body, readErr := io.ReadAll(request.Body)
					assert.NoError(t, readErr)
					assert.Equal(
						t,
						wantBody,
						body,
						"every attempt must register the same content-addressed schematic",
					)

					if calls.Add(1) <= testCase.failures {
						http.Error(writer, "fixture response mentions 503", testCase.status)

						return
					}

					writer.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(writer).
						Encode(map[string]string{"id": "stored-id", "schematic": string(body)})
				}),
			)
			t.Cleanup(server.Close)

			id, registerErr := talosprovisioner.RegisterSchematicForTest(
				t.Context(),
				server.URL,
				time.Second,
				computed,
			)

			if testCase.wantSuccess {
				require.NoError(t, registerErr)
				assert.Equal(t, "stored-id", id)
			} else {
				require.Error(t, registerErr)
				assert.Empty(t, id)
			}

			assert.Equal(t, testCase.wantCalls, calls.Load())
		})
	}
}

func TestRegisterSchematicRetriesInterruptedTransport(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, err := io.ReadAll(request.Body)
			assert.NoError(t, err)

			if calls.Add(1) < 3 {
				// A dropped connection exercises the SDK's real transport error, not a string fixture.
				hijacker, ok := writer.(http.Hijacker)
				if !assert.True(t, ok) {
					return
				}

				connection, _, hijackErr := hijacker.Hijack()
				if assert.NoError(t, hijackErr) {
					assert.NoError(t, connection.Close())
				}

				return
			}

			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).
				Encode(map[string]string{"id": "stored-id", "schematic": string(body)})
		}),
	)
	t.Cleanup(server.Close)

	id, err := talosprovisioner.RegisterSchematicForTest(
		t.Context(),
		server.URL,
		time.Second,
		talosconfigmanager.Schematic{},
	)

	require.NoError(t, err)
	assert.Equal(t, "stored-id", id)
	assert.Equal(t, int32(3), calls.Load())
}

func TestRegisterSchematicRetainsTheOverallDeadlineAcrossRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			http.Error(writer, "temporary failure", http.StatusServiceUnavailable)
		}),
	)
	t.Cleanup(server.Close)

	_, err := talosprovisioner.RegisterSchematicForTest(
		t.Context(),
		server.URL,
		50*time.Millisecond,
		talosconfigmanager.Schematic{},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(1), calls.Load(), "the original deadline must stop the backoff")
}

func TestRegisterSchematicCancelsBeforeRetrying(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	var calls atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			cancel()
			http.Error(writer, "temporary failure", http.StatusServiceUnavailable)
		}),
	)
	t.Cleanup(server.Close)

	_, err := talosprovisioner.RegisterSchematicForTest(
		ctx,
		server.URL,
		time.Second,
		talosconfigmanager.Schematic{},
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRegisterSchematicRejectsAnUntrustedFactoryCertificate(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewUnstartedServer(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
		}),
	)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)

	_, err := talosprovisioner.RegisterSchematicForTest(
		t.Context(),
		server.URL,
		time.Second,
		talosconfigmanager.Schematic{},
	)

	var certificateError *tls.CertificateVerificationError
	require.ErrorAs(t, err, &certificateError)
	assert.Zero(t, calls.Load(), "no schematic may be sent over unverified TLS")
}

//nolint:testpackage // Internal test needed to verify unexported retry helpers
package helm

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Static test errors for retry tests.
// Error messages intentionally match real HTTP error patterns including capitalization.
//
//nolint:staticcheck // Test error strings simulate real HTTP error messages with standard capitalization
var (
	errGeneric             = errors.New("something went wrong")
	errNotFound            = errors.New("404 Not Found")
	errBadRequest          = errors.New("400 Bad Request")
	errDownload500         = errors.New("failed to download: 500")
	errInternalServerError = errors.New("server returned Internal Server Error")
	errUpstream502         = errors.New("upstream returned 502")
	errBadGateway          = errors.New("Bad Gateway error occurred")
	errStatusCode503       = errors.New("got status code 503")
	errServiceUnavailable  = errors.New("Service Unavailable - try again later")
	errTimeout504          = errors.New("504 timeout from proxy")
	errGatewayTimeout      = errors.New("Gateway Timeout waiting for upstream")
	errWrapped500          = errors.New(
		"failed to download index: server returned 500 Internal Server Error",
	)
)

func TestIsRetryableHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "nil error", err: nil, expected: false},
		{name: "generic error", err: errGeneric, expected: false},
		{name: "404 not found", err: errNotFound, expected: false},
		{name: "400 bad request", err: errBadRequest, expected: false},
		{name: "500 internal server error code", err: errDownload500, expected: true},
		{name: "500 Internal Server Error text", err: errInternalServerError, expected: true},
		{name: "502 Bad Gateway code", err: errUpstream502, expected: true},
		{name: "502 Bad Gateway text", err: errBadGateway, expected: true},
		{name: "503 Service Unavailable code", err: errStatusCode503, expected: true},
		{name: "503 Service Unavailable text", err: errServiceUnavailable, expected: true},
		{name: "504 Gateway Timeout code", err: errTimeout504, expected: true},
		{name: "504 Gateway Timeout text", err: errGatewayTimeout, expected: true},
		{name: "wrapped 500 error", err: errWrapped500, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isRetryableHTTPError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateRepoRetryDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		attempt       int
		expectedDelay time.Duration
	}{
		{name: "first attempt", attempt: 1, expectedDelay: 2 * time.Second},
		{name: "second attempt", attempt: 2, expectedDelay: 4 * time.Second},
		{name: "third attempt", attempt: 3, expectedDelay: 8 * time.Second},
		{name: "fourth attempt capped", attempt: 4, expectedDelay: 15 * time.Second},
		{name: "large attempt at max", attempt: 10, expectedDelay: 15 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := calculateRepoRetryDelay(tt.attempt)
			assert.Equal(t, tt.expectedDelay, result)
		})
	}
}

func TestRetryConstants(t *testing.T) {
	t.Parallel()

	// Verify retry constants have sensible values
	assert.Equal(t, 3, repoIndexMaxRetries, "max retries should be 3")
	assert.Equal(t, 2*time.Second, repoIndexRetryBaseWait, "base wait should be 2s")
	assert.Equal(t, 15*time.Second, repoIndexRetryMaxWait, "max wait should be 15s")
}

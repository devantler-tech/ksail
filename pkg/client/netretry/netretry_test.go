package netretry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/netretry"
	"github.com/stretchr/testify/assert"
)

// Static test errors for retry tests.
// Error messages intentionally match real HTTP/network error patterns including capitalization.
var (
	errGeneric            = errors.New("something went wrong")
	errNotFound           = errors.New("404 Not Found")
	errBadRequest         = errors.New("400 Bad Request")
	errUnauthorized       = errors.New("unauthorized: authentication required")
	errConnectPort5000    = errors.New("connect to :5000")
	errDownload500        = errors.New("failed to download: 500")
	errInternalServer     = errors.New("server returned Internal Server Error")
	errUpstream502        = errors.New("upstream returned 502")
	errBadGateway         = errors.New("response: Bad Gateway error occurred")
	errStatusCode503      = errors.New("got status code 503")
	errServiceUnavailable = errors.New("response: Service Unavailable - try again later")
	errTimeout504         = errors.New("504 timeout from proxy")
	errGatewayTimeout     = errors.New("response: Gateway Timeout waiting for upstream")
	errWrapped500         = errors.New(
		"failed to download index: server returned 500 Internal Server Error",
	)
	errConnReset = errors.New(
		"read tcp 10.1.0.115:37414->98.84.224.111:443: read: connection reset by peer",
	)
	errConnRefused = errors.New(
		"dial tcp 127.0.0.1:443: connect: connection refused",
	)
	errIOTimeout = errors.New(
		"net/http: request canceled (Client.Timeout exceeded): i/o timeout",
	)
	errTLSTimeout    = errors.New("net/http: TLS handshake timeout")
	errUnexpectedEOF = errors.New("unexpected EOF")
	errNoSuchHost    = errors.New(
		"dial tcp: lookup charts.example.com: no such host",
	)
	errContextDeadline = errors.New(
		"context deadline exceeded (Client.Timeout exceeded while awaiting headers)",
	)
)

func TestIsRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		// Non-retryable cases.
		{name: "nil error", err: nil, expected: false},
		{name: "generic error", err: errGeneric, expected: false},
		{name: "404 not found", err: errNotFound, expected: false},
		{name: "400 bad request", err: errBadRequest, expected: false},
		{name: "auth error", err: errUnauthorized, expected: false},
		{name: "port 5000 not matched", err: errConnectPort5000, expected: false},
		// HTTP 5xx codes.
		{name: "500 code", err: errDownload500, expected: true},
		{name: "500 text", err: errInternalServer, expected: true},
		{name: "502 code", err: errUpstream502, expected: true},
		{name: "502 text", err: errBadGateway, expected: true},
		{name: "503 code", err: errStatusCode503, expected: true},
		{name: "503 text", err: errServiceUnavailable, expected: true},
		{name: "504 code", err: errTimeout504, expected: true},
		{name: "504 text", err: errGatewayTimeout, expected: true},
		{name: "wrapped 500", err: errWrapped500, expected: true},
		// TCP-level transient errors.
		{name: "connection reset by peer", err: errConnReset, expected: true},
		{name: "connection refused", err: errConnRefused, expected: true},
		{name: "i/o timeout", err: errIOTimeout, expected: true},
		{name: "TLS handshake timeout", err: errTLSTimeout, expected: true},
		{name: "unexpected EOF", err: errUnexpectedEOF, expected: true},
		{name: "no such host", err: errNoSuchHost, expected: true},
		{name: "context deadline exceeded", err: errContextDeadline, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := netretry.IsRetryable(tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExponentialDelay(t *testing.T) {
	t.Parallel()

	baseWait := 2 * time.Second
	maxWait := 15 * time.Second

	tests := []struct {
		name     string
		attempt  int
		expected time.Duration
	}{
		{name: "first attempt", attempt: 1, expected: 2 * time.Second},
		{name: "second attempt", attempt: 2, expected: 4 * time.Second},
		{name: "third attempt", attempt: 3, expected: 8 * time.Second},
		{name: "fourth attempt capped", attempt: 4, expected: 15 * time.Second},
		{name: "large attempt at max", attempt: 10, expected: 15 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := netretry.ExponentialDelay(tt.attempt, baseWait, maxWait)
			assert.Equal(t, tt.expected, got)
		})
	}
}

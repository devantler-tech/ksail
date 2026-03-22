// Package netretry provides shared retry utilities for transient network errors
// across Kubernetes client packages (Docker, Helm, etc.).
package netretry

import (
	"regexp"
	"strings"
	"time"
)

// httpStatusCodePattern matches HTTP 429 and 5xx status codes at word boundaries
// to avoid false positives on port numbers like ":5000".
var httpStatusCodePattern = regexp.MustCompile(`\b(429|5\d{2})\b`)

// redirectLimitPattern matches Go's HTTP client redirect limit errors
// (e.g., "stopped after 10 redirects").
var redirectLimitPattern = regexp.MustCompile(`stopped after \d+ redirects`)

// IsRetryable returns true if the error indicates a transient network error
// that should be retried. This covers HTTP 429 and 5xx status codes and TCP-level
// errors such as connection resets, timeouts, and unexpected EOF.
// Callers that need to handle additional domain-specific transient errors (e.g.,
// Copilot auth "fetch failed") should augment this function with a local helper.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// HTTP status text patterns and TCP-level transient network errors.
	textPatterns := []string{
		"Too Many Requests",
		"Internal Server Error", "Bad Gateway",
		"Service Unavailable", "Gateway Timeout",
		"connection reset by peer", "connection refused",
		"i/o timeout", "TLS handshake timeout",
		"unexpected EOF", "no such host",
		"context deadline exceeded",
	}

	for _, pattern := range textPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	// Match HTTP 429/5xx numeric codes at word boundaries to avoid false positives
	// on port numbers like ":5000". Uses regexp for precise matching.
	if httpStatusCodePattern.MatchString(errMsg) {
		return true
	}

	// Match redirect limit errors (e.g., "stopped after 10 redirects").
	return redirectLimitPattern.MatchString(errMsg)
}

// ExponentialDelay returns the delay for the given retry attempt
// using exponential backoff.
// Uses the formula: min(baseWait * 2^(attempt-1), maxWait).
func ExponentialDelay(
	attempt int,
	baseWait, maxWait time.Duration,
) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	return min(baseWait*time.Duration(1<<(attempt-1)), maxWait)
}

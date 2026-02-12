// Package netretry provides shared retry utilities for transient network errors
// across Kubernetes client packages (Docker, Helm, etc.).
package netretry

import (
	"regexp"
	"strings"
	"time"
)

// httpStatusCodePattern matches HTTP 5xx status codes at word boundaries
// to avoid false positives on port numbers like ":5000".
var httpStatusCodePattern = regexp.MustCompile(`\b50[0-4]\b`)

// IsRetryable returns true if the error indicates a transient network error
// that should be retried. This covers HTTP 5xx status codes and TCP-level errors
// such as connection resets, timeouts, and unexpected EOF.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// HTTP 5xx status text patterns and TCP-level transient network errors.
	textPatterns := []string{
		"Internal Server Error", "Bad Gateway",
		"Service Unavailable", "Gateway Timeout",
		"connection reset by peer", "connection refused",
		"i/o timeout", "TLS handshake timeout",
		"unexpected EOF", "no such host",
	}

	for _, pattern := range textPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	// Match HTTP 5xx numeric codes at word boundaries to avoid false positives
	// on port numbers like ":5000". Uses regexp for precise matching.
	return httpStatusCodePattern.MatchString(errMsg)
}

// ExponentialDelay returns the delay for the given retry attempt
// using exponential backoff.
// Uses the formula: min(baseWait * 2^(attempt-1), maxWait).
func ExponentialDelay(
	attempt int,
	baseWait, maxWait time.Duration,
) time.Duration {
	return min(baseWait*time.Duration(1<<(attempt-1)), maxWait)
}

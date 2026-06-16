// Package netretry provides shared retry utilities for transient network errors
// across Kubernetes client packages (Docker, Helm, etc.).
package netretry

import (
	"regexp"
	"strings"
	"time"
)

// transientTextPatterns contains fixed substrings for HTTP, TCP-level, and
// malformed-download transient errors. Declared at package level to avoid slice
// allocation on every IsRetryable call.
//
//nolint:gochecknoglobals // avoids per-call slice allocation in hot retry path
var transientTextPatterns = []string{
	"Too Many Requests",
	"Internal Server Error", "Bad Gateway",
	"Service Unavailable", "Gateway Timeout",
	"connection reset by peer", "connection refused",
	"i/o timeout", "TLS handshake timeout",
	"unexpected EOF", "no such host",
	"context deadline exceeded",
	// Malformed/truncated downloaded document: a flaky upstream (e.g. a CDN or
	// GitHub Pages) momentarily serving a partial Helm repository index.yaml
	// surfaces as a YAML->JSON conversion failure. A repository index is
	// server-generated, valid YAML, so a parse failure on the downloaded copy is
	// a transient truncation rather than a genuine config error — retry it. This
	// is specific to the sigs.k8s.io/yaml conversion wrapper used for downloaded
	// indexes/manifests; plain "yaml: unmarshal errors" (invalid user values)
	// lack this prefix and stay non-retryable. See #5257.
	"error converting YAML to JSON",
}

// httpStatusCodePattern matches HTTP 429 and 5xx status codes at word boundaries
// to avoid false positives on port numbers like ":5000".
var httpStatusCodePattern = regexp.MustCompile(`\b(429|5\d{2})\b`)

// redirectLimitPattern matches Go's HTTP client redirect limit errors
// (e.g., "stopped after 10 redirects").
var redirectLimitPattern = regexp.MustCompile(`stopped after \d+ redirects`)

// IsRetryable returns true if the error indicates a transient network error
// that should be retried. This covers HTTP 429 and 5xx status codes, TCP-level
// errors such as connection resets, timeouts, and unexpected EOF, and a
// truncated/malformed downloaded document (e.g. a partially-served Helm
// repository index) surfaced as a YAML->JSON conversion failure.
// Callers that need to handle additional domain-specific transient errors (e.g.,
// Copilot auth "fetch failed") should augment this function with a local helper.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// HTTP status text patterns and TCP-level transient network errors.
	for _, pattern := range transientTextPatterns {
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

	// Cap the exponent to avoid excessively large shifts and ensure predictable behavior
	// for very large attempt values. Beyond this, the delay is effectively saturated at maxWait.
	const maxExponent = 30
	if attempt-1 > maxExponent {
		return maxWait
	}

	delay := baseWait * time.Duration(1<<(attempt-1))

	return min(delay, maxWait)
}

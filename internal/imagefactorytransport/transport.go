// Package imagefactorytransport sizes writes without changing TLS security defaults.
package imagefactorytransport

import "net/http"

// Client preserves a custom default transport, or clones the standard transport.
func Client() http.Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.Client{Transport: http.DefaultTransport}
	}
	return http.Client{Transport: New(base)}
}

// New gives registration its own transport while preserving the caller's settings.
func New(base *http.Transport) *http.Transport {
	return base.Clone()
}

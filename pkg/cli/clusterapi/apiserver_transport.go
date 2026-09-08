package clusterapi

import (
	"fmt"
	"net/http"

	"k8s.io/client-go/rest"
)

// apiserverTransportFor builds the HTTP transport used for direct apiserver calls (watch, proxy).
//
// client-go's transport cache returns the process-global http.DefaultTransport whenever the config
// needs no custom TLS, dialer or proxy — which is every plain-HTTP apiserver config. A watch is
// long-lived, so sharing that pool means any goroutine calling
// http.DefaultTransport.CloseIdleConnections() can break the stream with
// "http: CloseIdleConnections called". Give apiserver calls their own connection pool instead.
//
// Only the process-global case is cloned. A config that needs a custom transport already gets a
// per-config cached one from client-go (not the process global), and may be wrapped in an auth
// RoundTripper that must not be copied, so it is passed through untouched.
func apiserverTransportFor(config *rest.Config) (http.RoundTripper, error) {
	transport, err := rest.TransportFor(config)
	if err != nil {
		return nil, fmt.Errorf("build apiserver transport: %w", err)
	}

	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok &&
		transport == http.DefaultTransport {
		return defaultTransport.Clone(), nil
	}

	return transport, nil
}

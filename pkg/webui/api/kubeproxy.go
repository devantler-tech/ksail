package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
)

// KubeProxyResponse is one proxied apiserver read: the upstream status and content type plus a
// streaming body the handler relays to the client. The caller must Close Body.
type KubeProxyResponse struct {
	Status      int
	ContentType string
	Body        io.ReadCloser
}

// KubeProxy is an optional interface a ClusterService may implement to proxy read-only requests to a
// cluster's kube-apiserver, so the SPA — and the Headlamp-compatible plugins' ApiProxy data layer —
// can read arbitrary resource kinds beyond the curated /resources allowlist. It is GET-only by design
// (a read-only window onto the apiserver, not a general passthrough), so it is not gated by the
// read-only guard (reads never mutate).
//
// Security note: this broadens the API beyond the curated resource allowlist to arbitrary apiserver
// reads with the caller's credentials. It is implemented only on the loopback-bound local
// `ksail ui`/desktop backend (where the caller already controls the kubeconfig); the operator leaves
// it unimplemented, so the route is not registered and the capability stays false there.
type KubeProxy interface {
	ProxyKubeGet(
		ctx context.Context,
		namespace, name, path string,
		query url.Values,
	) (KubeProxyResponse, error)
}

func (s *Server) handleKubeProxy(writer http.ResponseWriter, request *http.Request) {
	proxy, ok := s.Service.(KubeProxy)
	if !ok {
		writeClientError(writer, ErrNotSupported)

		return
	}

	result, err := proxy.ProxyKubeGet(
		request.Context(),
		request.PathValue("namespace"),
		request.PathValue("name"),
		request.PathValue("path"),
		request.URL.Query(),
	)
	if err != nil {
		writeClientError(writer, err)

		return
	}

	defer func() { _ = result.Body.Close() }()

	contentType := result.ContentType
	if contentType == "" {
		contentType = "application/json"
	}

	// nosniff keeps the upstream content type authoritative; the body is streamed (not buffered) so a
	// large list response does not balloon server memory.
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(result.Status)
	_, _ = io.Copy(writer, result.Body)
}

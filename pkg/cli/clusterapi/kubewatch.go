package clusterapi

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"k8s.io/client-go/rest"
)

// Ensure the local backend exposes the read-only kube-apiserver watch alongside the kube-proxy.
var _ api.KubeWatch = (*Service)(nil)

// WatchKube opens a streaming WATCH against the named cluster's kube-apiserver at apiPath, returning the
// upstream response body for the handler to stream as SSE. It is the streaming analogue of ProxyKubeGet:
// it reuses the same restConfigForCluster kubeconfig seam (so it honours the same credentials as the
// resource browser) and the apiserver's own transport (TLS + auth). Only GET is issued — read-only —
// watch=true is forced in the query, and the path is cleaned and fixed to the apiserver host, so a
// crafted path can never target a different host (no SSRF).
//
// No client timeout is set: a watch is intentionally long-lived. The handler bounds the stream's
// lifetime (idle timeout, max duration) and propagates ctx cancellation (client disconnect) to the
// underlying request, so the connection cannot run forever.
func (s *Service) WatchKube(
	ctx context.Context,
	_, name, apiPath string,
	query url.Values,
) (io.ReadCloser, error) {
	config, err := s.restConfigForCluster(name)
	if err != nil {
		return nil, fmt.Errorf("resolve cluster %q: %w", name, err)
	}

	transport, err := rest.TransportFor(config)
	if err != nil {
		return nil, fmt.Errorf("build apiserver transport: %w", err)
	}

	target, err := url.Parse(config.Host)
	if err != nil {
		return nil, fmt.Errorf("parse apiserver host: %w", err)
	}

	// Clean the requested path and join it onto the apiserver host. path.Clean collapses any ".."
	// components, and because the host comes from the resolved rest.Config (not the request), the
	// watch request can only ever reach the cluster's own apiserver.
	target.Path = path.Join(target.Path, path.Clean("/"+apiPath))

	// Force watch=true so the apiserver returns a streaming watch (newline-delimited JSON events)
	// regardless of what the caller passed, while preserving any other query params (e.g. labelSelector,
	// resourceVersion).
	forced := cloneQueryWithWatch(query)
	target.RawQuery = forced.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build watch request: %w", err)
	}

	request.Header.Set("Accept", "application/json")

	// No Timeout on the client: a watch is long-lived. ctx (cancelled on client disconnect / max
	// duration in the handler) bounds it instead.
	client := &http.Client{Transport: transport}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("open apiserver watch: %w", err)
	}

	// The body is returned to the caller (the handler), which closes it — bodyclose tracks the escape, so
	// no //nolint is needed here.
	return response.Body, nil
}

// cloneQueryWithWatch copies query and forces watch=true, so forcing the watch never mutates the
// caller's url.Values (the request's parsed query is shared with the handler).
func cloneQueryWithWatch(query url.Values) url.Values {
	forced := make(url.Values, len(query)+1)
	maps.Copy(forced, query)
	forced.Set("watch", "true")

	return forced
}

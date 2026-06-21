package clusterapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"k8s.io/client-go/rest"
)

// kubeProxyTimeout bounds a single proxied apiserver read.
const kubeProxyTimeout = 30 * time.Second

// Ensure the local backend exposes the read-only kube-apiserver proxy.
var _ api.KubeProxy = (*Service)(nil)

// ProxyKubeGet proxies a read-only GET to the named cluster's kube-apiserver at apiPath, returning the
// upstream response for the handler to stream. It reuses the single restConfigForCluster kubeconfig
// seam (so it honours the same credentials as the resource browser) and the apiserver's own transport
// (TLS + auth). Only GET is issued — read-only — and the path is cleaned and fixed to the apiserver
// host, so a crafted path can never target a different host (no SSRF).
func (s *Service) ProxyKubeGet(
	ctx context.Context,
	_, name, apiPath string,
	query url.Values,
) (api.KubeProxyResponse, error) {
	config, err := s.restConfigForCluster(name)
	if err != nil {
		return api.KubeProxyResponse{}, fmt.Errorf("resolve cluster %q: %w", name, err)
	}

	transport, err := rest.TransportFor(config)
	if err != nil {
		return api.KubeProxyResponse{}, fmt.Errorf("build apiserver transport: %w", err)
	}

	target, err := url.Parse(config.Host)
	if err != nil {
		return api.KubeProxyResponse{}, fmt.Errorf("parse apiserver host: %w", err)
	}

	// Clean the requested path and join it onto the apiserver host. path.Clean collapses any ".."
	// components, and because the host comes from the resolved rest.Config (not the request), the
	// proxied request can only ever reach the cluster's own apiserver.
	target.Path = path.Join(target.Path, path.Clean("/"+apiPath))
	target.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return api.KubeProxyResponse{}, fmt.Errorf("build proxy request: %w", err)
	}

	request.Header.Set("Accept", "application/json")

	client := &http.Client{Transport: transport, Timeout: kubeProxyTimeout}

	//nolint:bodyclose // The response body is returned to the caller (the handler), which closes it.
	response, err := client.Do(request)
	if err != nil {
		return api.KubeProxyResponse{}, fmt.Errorf("proxy apiserver request: %w", err)
	}

	return api.KubeProxyResponse{
		Status:      response.StatusCode,
		ContentType: response.Header.Get("Content-Type"),
		Body:        response.Body,
	}, nil
}

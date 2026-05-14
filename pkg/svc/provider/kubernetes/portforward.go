package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardSession manages a local TCP tunnel to a Kubernetes pod.
//
// It supports two backends:
//   - SPDY port-forward (tcpProxy): efficient for regular HTTP/HTTPS traffic.
//   - Exec tunnel (execTunnel): required for services that use HTTP connection
//     hijacking (e.g., Docker daemon's exec API), where SPDY port-forward's
//     half-close semantics cause premature stream termination.
type PortForwardSession struct {
	// LocalPort is the local port that was allocated.
	LocalPort int

	proxy  *tcpProxy
	tunnel *execTunnel
}

// Close stops the session and releases all resources.
func (s *PortForwardSession) Close() {
	if s.proxy != nil {
		s.proxy.close()
	}

	if s.tunnel != nil {
		s.tunnel.close()
	}
}

// StartPortForward opens a resilient port-forward from a random local port to the specified
// pod port. It returns a PortForwardSession that must be closed when no longer needed.
func (p *Provider) StartPortForward(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	podName string,
	podPort int,
) (*PortForwardSession, error) {
	namespace := NamespaceName(clusterName)

	return startProxy(ctx, restConfig, namespace, podName, podPort)
}

// StartPortForwardInNamespace opens a resilient port-forward from a random local port to the
// specified pod port in an arbitrary namespace.
func (p *Provider) StartPortForwardInNamespace(
	ctx context.Context,
	restConfig *rest.Config,
	namespace string,
	podName string,
	podPort int,
) (*PortForwardSession, error) {
	return startProxy(ctx, restConfig, namespace, podName, podPort)
}

// StartExecTunnel opens a TCP tunnel from a random local port to the specified
// pod port by exec-ing `nc localhost <port>` inside the container for each
// connection.
//
// Use this instead of StartPortForward when the target service uses HTTP
// connection hijacking (101 Switching Protocols), such as Docker's exec API.
// SPDY port-forward's half-close semantics break hijacked connections, but
// CRI exec with separate stdin/stdout streams handles them correctly.
func (p *Provider) StartExecTunnel(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	podName string,
	containerName string,
	podPort int,
) (*PortForwardSession, error) {
	namespace := NamespaceName(clusterName)

	return startExecTunnel(ctx, p.client, restConfig, namespace, podName, containerName, podPort)
}

// startExecTunnel creates an exec-based tunnel and verifies it's functional.
func startExecTunnel(
	ctx context.Context,
	clientset kubernetes.Interface,
	restConfig *rest.Config,
	namespace, podName, containerName string,
	podPort int,
) (*PortForwardSession, error) {
	tunnel, err := newExecTunnel(clientset, restConfig, namespace, podName, containerName, podPort)
	if err != nil {
		return nil, fmt.Errorf("create exec tunnel: %w", err)
	}

	select {
	case <-ctx.Done():
		tunnel.close()

		return nil, fmt.Errorf("exec tunnel timed out: %w", ctx.Err())
	default:
	}

	go tunnel.run()

	return &PortForwardSession{
		LocalPort: tunnel.localPort,
		tunnel:    tunnel,
	}, nil
}

// startProxy creates a SPDY dialer for the given pod, starts a TCP proxy with
// auto-reconnecting SPDY backend, and verifies the initial connection.
func startProxy(
	ctx context.Context,
	restConfig *rest.Config,
	namespace, podName string,
	podPort int,
) (*PortForwardSession, error) {
	apiURL, err := url.Parse(restConfig.Host)
	if err != nil {
		return nil, fmt.Errorf("parse API server URL: %w", err)
	}

	apiURL.Path = fmt.Sprintf(
		"/api/v1/namespaces/%s/pods/%s/portforward",
		namespace, podName,
	)

	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create SPDY transport: %w", err)
	}

	dialer := spdy.NewDialer(
		upgrader,
		&http.Client{Transport: transport},
		http.MethodPost,
		apiURL,
	)

	proxy, err := newTCPProxy(dialer, podPort)
	if err != nil {
		return nil, fmt.Errorf("create TCP proxy: %w", err)
	}

	// Verify the SPDY connection is functional before returning
	if _, connErr := proxy.getConnection(); connErr != nil {
		proxy.close()

		return nil, fmt.Errorf("initial SPDY dial: %w", connErr)
	}

	go proxy.run()

	// Verify the proxy is accepting connections
	select {
	case <-ctx.Done():
		proxy.close()

		return nil, fmt.Errorf("port-forward timed out: %w", ctx.Err())
	default:
	}

	return &PortForwardSession{
		LocalPort: proxy.localPort,
		proxy:     proxy,
	}, nil
}

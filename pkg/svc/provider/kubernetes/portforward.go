package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardSession manages a SPDY-tunneled port-forward to a pod.
// It forwards a local port to a remote pod port through the Kubernetes API server.
type PortForwardSession struct {
	// LocalPort is the local port that was allocated.
	LocalPort int

	stopCh  chan struct{}
	readyCh chan struct{}
}

// Close stops the port-forward session.
func (s *PortForwardSession) Close() {
	select {
	case <-s.stopCh:
		// Already closed
	default:
		close(s.stopCh)
	}
}

// StartPortForward opens a port-forward from a random local port to the specified
// pod port. It returns a PortForwardSession that must be closed when no longer needed.
// The DOCKER_HOST address is available as tcp://localhost:<LocalPort>.
func (p *Provider) StartPortForward(
	ctx context.Context,
	restConfig *rest.Config,
	clusterName string,
	podName string,
	podPort int,
) (*PortForwardSession, error) {
	ns := NamespaceName(clusterName)

	// Build the URL for the pod's portforward subresource
	apiURL, err := url.Parse(restConfig.Host)
	if err != nil {
		return nil, fmt.Errorf("parse API server URL: %w", err)
	}

	apiURL.Path = fmt.Sprintf(
		"/api/v1/namespaces/%s/pods/%s/portforward",
		ns, podName,
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

	// Allocate a random local port
	localPort, err := getAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("allocate local port: %w", err)
	}

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	portSpec := fmt.Sprintf("%d:%d", localPort, podPort)

	fw, err := portforward.New(
		dialer,
		[]string{portSpec},
		stopCh,
		readyCh,
		io.Discard, // stdout — suppress portforward chatter
		io.Discard, // stderr
	)
	if err != nil {
		return nil, fmt.Errorf("create port-forwarder: %w", err)
	}

	errCh := make(chan error, 1)

	go func() {
		errCh <- fw.ForwardPorts()
	}()

	// Wait for the port-forward to be ready or fail
	select {
	case <-readyCh:
		// Port-forward is ready
	case err := <-errCh:
		return nil, fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		close(stopCh)

		return nil, fmt.Errorf("port-forward timed out: %w", ctx.Err())
	}

	// Retrieve the actual local port (in case :0 was used)
	ports, err := fw.GetPorts()
	if err == nil && len(ports) > 0 {
		localPort = int(ports[0].Local)
	}

	return &PortForwardSession{
		LocalPort: localPort,
		stopCh:    stopCh,
		readyCh:   readyCh,
	}, nil
}

// StartPortForwardInNamespace opens a port-forward from a random local port to the specified
// pod port in an arbitrary namespace. Use this for k3k or other operators that use custom namespaces.
func (p *Provider) StartPortForwardInNamespace(
	ctx context.Context,
	restConfig *rest.Config,
	namespace string,
	podName string,
	podPort int,
) (*PortForwardSession, error) {
	// Build the URL for the pod's portforward subresource
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

	// Allocate a random local port
	localPort, err := getAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("allocate local port: %w", err)
	}

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	portSpec := fmt.Sprintf("%d:%d", localPort, podPort)

	fw, err := portforward.New(
		dialer,
		[]string{portSpec},
		stopCh,
		readyCh,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		return nil, fmt.Errorf("create port-forwarder: %w", err)
	}

	errCh := make(chan error, 1)

	go func() {
		errCh <- fw.ForwardPorts()
	}()

	select {
	case <-readyCh:
	case err := <-errCh:
		return nil, fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		close(stopCh)

		return nil, fmt.Errorf("port-forward timed out: %w", ctx.Err())
	}

	ports, err := fw.GetPorts()
	if err == nil && len(ports) > 0 {
		localPort = int(ports[0].Local)
	}

	return &PortForwardSession{
		LocalPort: localPort,
		stopCh:    stopCh,
		readyCh:   readyCh,
	}, nil
}

// getAvailablePort finds an available TCP port on localhost.
func getAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find available port: %w", err)
	}

	addr := listener.Addr().String()

	err = listener.Close()
	if err != nil {
		return 0, fmt.Errorf("close port listener: %w", err)
	}

	// Extract port from addr "127.0.0.1:PORT"
	parts := strings.Split(addr, ":")
	if len(parts) < 2 {
		return 0, fmt.Errorf("unexpected address format: %s", addr)
	}

	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0, fmt.Errorf("parse port number: %w", err)
	}

	return port, nil
}

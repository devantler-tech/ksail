package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// execTunnel is a local TCP proxy that tunnels connections to a pod port
// by exec-ing `nc localhost <port>` inside the target container.
//
// Unlike SPDY port-forward (which breaks Docker exec's HTTP connection
// hijacking due to premature SPDY stream half-close), the exec-based tunnel
// uses CRI exec with separate stdin/stdout SPDY streams. This correctly
// handles bidirectional streaming for protocols that use HTTP Upgrade
// (101 Switching Protocols), such as Docker's exec API.
//
// Each accepted TCP connection spawns a new `nc` process inside the pod.
// The connection's bytes are piped to nc's stdin, and nc's stdout is piped
// back to the connection. When either side closes, nc exits and the exec
// session terminates cleanly.
type execTunnel struct {
	listener   net.Listener
	localPort  int
	clientset  kubernetes.Interface
	restConfig *rest.Config
	namespace  string
	podName    string
	container  string
	targetPort int
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// newExecTunnel creates a TCP tunnel listening on a random local port.
// Each connection is relayed to the pod via CRI exec + nc.
func newExecTunnel(
	clientset kubernetes.Interface,
	restConfig *rest.Config,
	namespace, podName, container string,
	targetPort int,
) (*execTunnel, error) {
	ll, err := newLocalListener()
	if err != nil {
		return nil, fmt.Errorf("exec tunnel: %w", err)
	}

	return &execTunnel{
		listener:   ll.Listener,
		localPort:  ll.Port,
		clientset:  clientset,
		restConfig: restConfig,
		namespace:  namespace,
		podName:    podName,
		container:  container,
		targetPort: targetPort,
		stopCh:     make(chan struct{}),
	}, nil
}

// run accepts TCP connections and tunnels each to the pod via exec.
// It blocks until the tunnel is closed.
func (t *execTunnel) run() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.stopCh:
				return
			default:
				continue
			}
		}

		t.wg.Add(1)

		go func() {
			defer t.wg.Done()

			t.handleConnection(conn)
		}()
	}
}

// close stops the tunnel and waits for active connections to drain.
func (t *execTunnel) close() {
	select {
	case <-t.stopCh:
		return
	default:
		close(t.stopCh)
	}

	t.listener.Close()
	t.wg.Wait()
}

// handleConnection tunnels a single TCP connection to the pod by exec-ing
// `nc localhost <targetPort>` and piping stdin/stdout.
func (t *execTunnel) handleConnection(conn net.Conn) {
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stop exec when the tunnel is shutting down
	go func() {
		select {
		case <-t.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	req := t.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(t.podName).
		Namespace(t.namespace).
		SubResource("exec")

	execOpts := &corev1.PodExecOptions{
		Container: t.container,
		Command:   []string{"nc", "localhost", strconv.Itoa(t.targetPort)},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
	}

	req.VersionedParams(execOpts, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(t.restConfig, "POST", req.URL())
	if err != nil {
		return
	}

	_ = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  conn,
		Stdout: conn,
		Stderr: io.Discard,
	})
}

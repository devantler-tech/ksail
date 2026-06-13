package clusterapi

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// Ensure the local backend can exec into pods.
var _ api.ExecService = (*Service)(nil)

// execClientFunc builds a clientset + rest.Config for a named cluster (pod exec needs both: the
// clientset to build the exec request URL, and the rest.Config for the SPDY executor). Injectable.
type execClientFunc func(ctx context.Context, clusterName string) (kubernetes.Interface, *rest.Config, error)

// defaultExecClient resolves the cluster's kubeconfig context (via the single restConfigForCluster
// seam) and builds a clientset + rest.Config.
func (s *Service) defaultExecClient(
	_ context.Context,
	clusterName string,
) (kubernetes.Interface, *rest.Config, error) {
	restConfig, err := s.restConfigForCluster(clusterName)
	if err != nil {
		return nil, nil, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("build clientset for %q: %w", clusterName, err)
	}

	return clientset, restConfig, nil
}

// resizeQueue adapts the API's resize channel to remotecommand.TerminalSizeQueue. Next blocks until a
// resize arrives and returns nil when the channel closes (client disconnected), ending the queue.
type resizeQueue struct {
	resize <-chan api.TerminalSize
}

func (q resizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.resize
	if !ok {
		return nil
	}

	return &remotecommand.TerminalSize{Width: size.Cols, Height: size.Rows}
}

// Exec opens an interactive (TTY) exec session into a pod container and bridges the supplied streams.
// stderr is merged into stdout (TTY), and resize events drive the terminal size.
func (s *Service) Exec(
	ctx context.Context,
	_, name string,
	request api.ExecRequest,
	streams api.ExecStreams,
) error {
	clientset, restConfig, err := s.newExecClient(ctx, name)
	if err != nil {
		return err
	}

	execRequest := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(request.Pod).
		Namespace(request.Namespace).
		SubResource("exec")

	execRequest.VersionedParams(&corev1.PodExecOptions{
		Container: request.Container,
		Command:   request.Command,
		Stdin:     true,
		Stdout:    true,
		Stderr:    false,
		TTY:       true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", execRequest.URL())
	if err != nil {
		return fmt.Errorf("create exec executor: %w", err)
	}

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             streams.Stdin,
		Stdout:            streams.Stdout,
		Tty:               true,
		TerminalSizeQueue: resizeQueue{resize: streams.Resize},
	})
	if err != nil {
		return fmt.Errorf("exec stream: %w", err)
	}

	return nil
}

package clusterapi

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// Ensure the local backend can stream pod logs.
var _ api.LogService = (*Service)(nil)

// logClientFunc builds a clientset for a named cluster (pod logs use CoreV1().Pods().GetLogs()).
// Injectable so tests can substitute a fake clientset instead of resolving a real kubeconfig context.
type logClientFunc func(ctx context.Context, clusterName string) (kubernetes.Interface, error)

// defaultLogClient builds a clientset for a local cluster from the single restConfigForCluster seam
// (rest.Config + kubernetes.NewForConfig — identical to the former k8s.NewClientset path).
func (s *Service) defaultLogClient(
	_ context.Context,
	clusterName string,
) (kubernetes.Interface, error) {
	restConfig, err := s.restConfigForCluster(clusterName)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build clientset for %q: %w", clusterName, err)
	}

	return clientset, nil
}

// PodLogs streams a pod container's logs from the named cluster. TailLines > 0 bounds the initial
// backlog; Follow keeps the stream open for new lines. The caller closes the returned stream.
func (s *Service) PodLogs(
	ctx context.Context,
	_, name string,
	request api.LogRequest,
) (io.ReadCloser, error) {
	clientset, err := s.newLogClient(ctx, name)
	if err != nil {
		return nil, err
	}

	options := &corev1.PodLogOptions{
		Container: request.Container,
		Follow:    request.Follow,
	}
	if request.TailLines > 0 {
		tail := request.TailLines
		options.TailLines = &tail
	}

	stream, err := clientset.CoreV1().Pods(request.Namespace).
		GetLogs(request.Pod, options).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("stream logs for %s/%s: %w", request.Namespace, request.Pod, err)
	}

	return stream, nil
}

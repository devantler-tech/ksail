package api

import (
	"context"
	"io"
)

// LogRequest selects which pod container to stream logs from.
type LogRequest struct {
	// Namespace is the pod's namespace.
	Namespace string
	// Pod is the pod name.
	Pod string
	// Container selects a container; empty means the pod's default container.
	Container string
	// Follow keeps the stream open and emits new lines as they are written (kubectl logs -f).
	Follow bool
	// TailLines, when > 0, starts the stream from the last N lines instead of the beginning.
	TailLines int64
}

// LogService is an optional interface a ClusterService may implement to stream a pod container's logs
// (the in-browser log viewer). When the serving ClusterService implements it, the server registers
// the logs SSE endpoint and advertises capabilities.workloadLogs=true; otherwise the endpoint 404s and
// the SPA hides the Logs action.
//
// Reading logs is non-mutating, so — unlike exec — it is permitted in read-only mode. The returned
// stream is the raw log byte stream (the handler frames each line as an SSE event); the caller closes
// it. The local backend resolves a clientset from the cluster's kubeconfig context; the operator does
// not implement it.
type LogService interface {
	PodLogs(ctx context.Context, namespace, name string, request LogRequest) (io.ReadCloser, error)
}

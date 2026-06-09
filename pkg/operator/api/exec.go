package api

import (
	"context"
	"io"
)

// TerminalSize is a terminal resize event (in character cells).
type TerminalSize struct {
	Rows uint16
	Cols uint16
}

// ExecRequest identifies the pod/container to exec into and the command to run.
type ExecRequest struct {
	Namespace string
	Pod       string
	Container string
	Command   []string
}

// ExecStreams carries the bidirectional streams for an exec session. Stdin/Stdout bridge the client's
// terminal; Resize delivers terminal resize events (closed when the client disconnects). The session
// runs with a TTY, so stderr is merged into Stdout.
type ExecStreams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Resize <-chan TerminalSize
}

// ExecService is an optional interface a ClusterService may implement to exec into a pod container
// (the in-browser terminal). When the serving ClusterService implements it, the server registers the
// exec WebSocket endpoint and advertises capabilities.workloadExec=true. Exec can run arbitrary
// commands in a workload, so the handler additionally refuses it in read-only mode and the SPA gates
// it on !readOnly. The operator does not implement it (no live-cluster client); the local backend
// resolves a client from the cluster's kubeconfig context.
type ExecService interface {
	Exec(
		ctx context.Context,
		clusterNamespace, clusterName string,
		request ExecRequest,
		streams ExecStreams,
	) error
}

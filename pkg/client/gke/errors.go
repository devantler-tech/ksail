package gke

import "errors"

var (
	// ErrNilCluster is returned when CreateCluster is called without a cluster
	// specification. The GKE API requires one, so the misconfiguration is caught
	// before any request is made.
	ErrNilCluster = errors.New("gke: a cluster specification is required")

	// ErrOperationFailed is returned when a long-running GKE operation completes
	// unsuccessfully. The wrapped message carries the operation's own error
	// detail so the caller sees what GKE reported.
	ErrOperationFailed = errors.New("gke: cluster operation failed")
)

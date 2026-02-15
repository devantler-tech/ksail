package vclusterprovisioner

import "errors"

// Static errors for the vcluster provisioner package.
var (
	// ErrNoVClusterNodes is returned when no VCluster nodes are found for a cluster.
	ErrNoVClusterNodes = errors.New("no VCluster nodes found for cluster")

	// ErrExecFailed is returned when a container exec command fails.
	ErrExecFailed = errors.New("exec failed")
)

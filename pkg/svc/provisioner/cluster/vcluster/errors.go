package vclusterprovisioner

import (
	"errors"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
)

// Static errors for the vcluster provisioner package.
var (
	// ErrNoVClusterNodes is returned when no VCluster nodes are found for a cluster.
	ErrNoVClusterNodes = errors.New("no VCluster nodes found for cluster")

	// ErrExecFailed is re-exported from the registry package for consistency.
	ErrExecFailed = registry.ErrExecFailed
)

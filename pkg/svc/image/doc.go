// Package image provides services for exporting and importing container images
// to and from Kubernetes cluster containerd runtimes.
//
// This package supports the following Kubernetes distributions:
//   - Vanilla (Kind): Uses Docker exec to run ctr commands in Kind nodes
//   - K3s (K3d): Uses Docker exec to run ctr commands in K3d nodes
//
// Note: Talos distribution is NOT supported for image export/import operations.
// Talos is an immutable OS without shell access, and its Machine API does not
// expose image export functionality. Only ImageList and ImagePull APIs are available.
//
// All operations use Go libraries only (Docker SDK) and do not rely on any
// binaries installed on the host machine.
package image

import "errors"

// Sentinel errors for the image package.
var (
	// ErrExecFailed is returned when a container exec command fails.
	ErrExecFailed = errors.New("container exec failed")
	// ErrInvalidClusterName is returned when a cluster name cannot be determined from context.
	ErrInvalidClusterName = errors.New("unable to determine cluster name from context")
	// ErrFileNotFoundInArchive is returned when an expected file is not found in a tar archive.
	ErrFileNotFoundInArchive = errors.New("file not found in tar archive")
	// ErrNoK8sNodesFound is returned when no valid Kubernetes nodes are found.
	ErrNoK8sNodesFound = errors.New("no valid kubernetes nodes found for image operations")
	// ErrInputFileNotFound is returned when the input file does not exist.
	ErrInputFileNotFound = errors.New("input file does not exist")
	// ErrUnsupportedDistribution is returned when the distribution does not support image operations.
	ErrUnsupportedDistribution = errors.New("distribution does not support image export/import")
)

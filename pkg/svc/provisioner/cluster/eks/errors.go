package eksprovisioner

import "errors"

// Static errors for the EKS provisioner package.
var (
	// ErrNoEKSNodes is returned when no EKS nodes are found for a cluster.
	ErrNoEKSNodes = errors.New("no EKS nodes found for cluster")

	// ErrEKSCreateFailed is returned when EKS cluster creation fails.
	ErrEKSCreateFailed = errors.New("EKS cluster creation failed")

	// ErrEKSDeleteFailed is returned when EKS cluster deletion fails.
	ErrEKSDeleteFailed = errors.New("EKS cluster deletion failed")

	// ErrBootstrapClusterFailed is returned when bootstrap cluster operations fail.
	ErrBootstrapClusterFailed = errors.New("bootstrap cluster operation failed")

	// ErrClusterSpecInvalid is returned when the EKS cluster spec is invalid.
	ErrClusterSpecInvalid = errors.New("invalid EKS cluster spec")
)

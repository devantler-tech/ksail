package eksctl

import "errors"

// ErrBinaryNotFound is returned when the `eksctl` binary cannot be located
// on PATH and the caller did not inject a custom binary path or Runner.
// Cluster creation on the EKS distribution is delegated to the eksctl CLI;
// see https://eksctl.io/installation/ for installation instructions.
var ErrBinaryNotFound = errors.New(
	"eksctl binary not found on PATH; install from https://eksctl.io/installation/",
)

// ErrExecFailed wraps a non-zero exit from the eksctl binary.
var ErrExecFailed = errors.New("eksctl command failed")

// ErrClusterNotFound is returned when an `eksctl get cluster` lookup for a
// named cluster returns an empty result set.
var ErrClusterNotFound = errors.New("eks cluster not found")

// ErrEmptyClusterName is returned when an operation requires a cluster name
// but an empty string was supplied.
var ErrEmptyClusterName = errors.New("cluster name must not be empty")

// ErrEmptyConfigPath is returned when a config-file-based operation is
// invoked without a config path.
var ErrEmptyConfigPath = errors.New("config path must not be empty")

// ErrEmptyNodegroupName is returned when a nodegroup operation requires a
// nodegroup name but an empty string was supplied.
var ErrEmptyNodegroupName = errors.New("nodegroup name must not be empty")

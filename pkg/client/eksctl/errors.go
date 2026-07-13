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

// ErrRunnerEnvironmentUnsupported is returned when a client has an explicit
// child environment but its injected legacy Runner cannot accept one. Failing
// closed prevents a test/custom runner from silently inheriting the wrong AWS
// identity; existing runners remain source-compatible when no environment is set.
var ErrRunnerEnvironmentUnsupported = errors.New(
	"eksctl runner does not support an explicit environment",
)

// ErrIncompleteStaticCredentials is returned when an explicit child
// environment contains only part of the AWS static credential tuple.
var ErrIncompleteStaticCredentials = errors.New("incomplete static AWS credentials")

// ErrExplicitCredentialsUnavailable is returned when a credential-isolated
// client was required to use resolved values but neither a profile nor a
// complete static credential pair was available.
var ErrExplicitCredentialsUnavailable = errors.New(
	"configured AWS credential sources resolved no credentials",
)

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

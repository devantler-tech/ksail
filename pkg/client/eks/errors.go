package eks

import "errors"

// ErrClusterNotFound is returned when DescribeCluster succeeds but the
// response carries no cluster payload.
var ErrClusterNotFound = errors.New("eks cluster not found")

// ErrIncompleteStaticCredentials is returned when an explicit AWS credential
// selection contains only part of the access-key/secret-key pair (or a session
// token without that pair). Failing here prevents fallback to an ambient identity.
var ErrIncompleteStaticCredentials = errors.New("incomplete explicit AWS static credentials")

// ErrExplicitCredentialsUnavailable is returned when custom credential
// sources were selected but resolved no usable profile or static key pair.
var ErrExplicitCredentialsUnavailable = errors.New(
	"explicit AWS credential selection resolved no credentials",
)

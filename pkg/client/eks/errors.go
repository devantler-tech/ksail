package eks

import "errors"

// ErrClusterNotFound is returned when DescribeCluster succeeds but the
// response carries no cluster payload.
var ErrClusterNotFound = errors.New("eks cluster not found")

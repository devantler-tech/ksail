// Package clustererrors provides common error types for cluster provisioners.
//
// This package defines sentinel errors that are shared across different
// cluster provisioner implementations (Kind, K3d, Talos) to enable
// consistent error handling in command handlers.
package clustererrors

import "errors"

// ErrClusterNotFound is returned when a cluster operation is attempted on a non-existent cluster.
// This error is used by all provisioner implementations (Kind, K3d, Talos) when attempting
// to delete, start, or stop a cluster that does not exist.
var ErrClusterNotFound = errors.New("cluster not found")

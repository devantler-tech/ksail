package k8s

import "errors"

// ErrKubeconfigPathEmpty is returned when kubeconfig path is empty.
var ErrKubeconfigPathEmpty = errors.New("kubeconfig path is empty")

// ErrKubeconfigNoCurrentContext is returned when a kubeconfig has no current context
// and multiple context entries, making it ambiguous which context to rename.
var ErrKubeconfigNoCurrentContext = errors.New("kubeconfig has no current context")

// ErrKubeconfigContextNotFound is returned when the specified context name
// does not exist in the kubeconfig.
var ErrKubeconfigContextNotFound = errors.New("kubeconfig context not found")

// ErrKubeconfigContextCollision is returned when the desired context name
// already exists as a different context entry in the kubeconfig.
var ErrKubeconfigContextCollision = errors.New("kubeconfig context name collision")

// ErrClusterEntryNotFound is returned when a cluster entry is not found in the kubeconfig.
var ErrClusterEntryNotFound = errors.New("cluster entry not found in kubeconfig")

// ErrAPIServerTimeout is returned when the API server does not become ready within the timeout.
var ErrAPIServerTimeout = errors.New("API server not ready within timeout")

// ErrClusterNotReady is returned when the cluster does not become ready
// (API reachable and a basic authorized read succeeds) within the timeout.
var ErrClusterNotReady = errors.New("cluster not ready within timeout")

package k8s

import "errors"

// ErrKubeconfigPathEmpty is returned when kubeconfig path is empty.
var ErrKubeconfigPathEmpty = errors.New("kubeconfig path is empty")

// ErrTimeoutExceeded is returned when a timeout is exceeded.
var ErrTimeoutExceeded = errors.New("timeout exceeded")

var errUnknownResourceType = errors.New("unknown resource type")

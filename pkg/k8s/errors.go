package k8s

import "errors"

// ErrKubeconfigPathEmpty is returned when kubeconfig path is empty.
var ErrKubeconfigPathEmpty = errors.New("kubeconfig path is empty")

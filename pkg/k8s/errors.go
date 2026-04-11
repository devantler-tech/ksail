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

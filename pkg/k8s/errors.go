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

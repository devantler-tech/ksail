// Package kubeconfighook provides a centralized hook that transparently refreshes
// Omni kubeconfig tokens before they expire. The hook is wired into Cobra's
// PersistentPreRunE so that every CLI command automatically gets a fresh token
// when needed.
package kubeconfighook

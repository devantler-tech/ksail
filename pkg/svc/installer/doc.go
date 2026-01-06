// Package installer provides functionality for installing and uninstalling components.
//
// This package defines the Installer interface and provides implementations
// for installing various Kubernetes components on clusters:
//
//   - argocd: ArgoCD GitOps engine installation
//   - cert-manager: Certificate management installation
//   - cni: Container Network Interface installers (Cilium, Calico)
//   - flux: Flux GitOps engine installation
//   - localpathstorage: Local path storage provisioner
//   - metrics-server: Kubernetes metrics server
//
// Common utilities:
//   - Installer interface for consistent installation patterns
//   - Helm chart deployment helpers
//   - Readiness polling for installed components
package installer

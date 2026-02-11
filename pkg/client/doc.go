// Package client provides embedded Kubernetes and container tool clients.
//
// This package contains Go library wrappers for various tools that are
// embedded directly into KSail, eliminating external binary dependencies:
//
//   - argocd: ArgoCD application and repository management
//   - docker: Docker container and registry operations
//   - flux: Flux CD GitOps controller client
//   - helm: Helm chart installation and management
//   - k9s: Terminal UI for Kubernetes cluster interaction
//   - kubeconform: Kubernetes manifest validation
//   - kubectl: Kubernetes API operations
//   - kustomize: Kustomize manifest rendering
//   - oci: OCI artifact building and pushing
//   - reconciler: Common base for GitOps reconciliation clients
//
// By embedding these clients as Go libraries, KSail only requires Docker
// as an external dependency, simplifying installation and ensuring
// version consistency across all components.
package client

// Package client provides Kubernetes and container tool clients.
//
// This package contains Go library wrappers for various tools, most of which
// are embedded directly into KSail as Go libraries:
//
//   - argocd: ArgoCD application and repository management
//   - docker: Docker container and registry operations
//   - eksctl: eksctl CLI shim for EKS cluster management (shells out to an
//     external eksctl binary)
//   - flux: Flux CD GitOps controller client
//   - helm: Helm chart installation and management
//   - k9s: Terminal UI for Kubernetes cluster interaction
//   - klogutil: klog output redirection and suppression utilities
//   - kubeconform: Kubernetes manifest validation
//   - kubectl: Kubernetes API operations
//   - kubescape: Kubescape security and compliance scanning
//   - kustomize: Kustomize manifest rendering
//   - netretry: Retry helpers for transient network failures
//   - oci: OCI artifact building and pushing
//   - reconciler: Common base for GitOps reconciliation clients
//   - sops: SOPS secret encryption, decryption, and editing
//
// By embedding these clients as Go libraries, KSail only requires Docker as
// an external dependency for local clusters, simplifying installation and
// ensuring version consistency across all components. The one exception is
// the EKS distribution: the eksctl subpackage shells out to an eksctl binary
// installed on the host instead of embedding it.
package client

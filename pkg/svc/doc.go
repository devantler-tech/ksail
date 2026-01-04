// Package svc provides service layer components for KSail.
//
// This package contains the business logic layer that coordinates between
// the CLI commands and the underlying clients/infrastructure:
//
//   - installer: Component installers for CNI, CSI, GitOps engines, and cert-manager
//   - provisioner: Cluster and registry provisioning for Kind, K3d, and Talos
//
// The svc package follows the service pattern, encapsulating complex operations
// that involve multiple steps, retries, and coordination between different
// subsystems.
//
// Key responsibilities:
//   - Installing and configuring Kubernetes components (Cilium, Calico, Flux, ArgoCD)
//   - Provisioning and managing cluster lifecycle (create, start, stop, delete)
//   - Managing container registries (local and mirror registries)
//   - Handling distribution-specific logic for Kind, K3d, and Talos
package svc

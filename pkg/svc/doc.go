// Package svc provides service layer components for KSail.
//
// This package contains the business logic layer that coordinates between
// the CLI commands and the underlying clients/infrastructure.
//
// Subpackages:
//   - chat: AI chat integration with GitHub Copilot SDK
//   - detector: Installed component and cluster distribution detection
//   - diff: Configuration diff engine for cluster updates
//   - image: Container image export/import for Kind and K3d nodes
//   - installer: Component installers for CNI, CSI, GitOps engines, and cert-manager
//   - mcp: Model Context Protocol server for AI assistants
//   - provider: Infrastructure providers (Docker, Hetzner)
//   - provisioner: Cluster and registry provisioning for Vanilla, K3s, and Talos
//   - registryresolver: OCI registry detection, resolution, and artifact push
//   - state: Cluster state tracking for update operations
package svc

// Package scaffolder provides utilities for scaffolding KSail project files and configuration.
//
// This package handles the generation and initialization of KSail project structures,
// including creating cluster configuration files, directory structures, and registry
// configurations for different Kubernetes distributions (Kind, K3d).
//
// Key functionality:
//   - Scaffold: Main orchestration for project file generation
//   - GenerateContainerdPatches: Kind mirror registry configuration
//   - GenerateK3dRegistryConfig: K3d mirror registry configuration
//   - CreateK3dConfig: K3d-specific configuration with CNI and metrics-server settings
//   - Distribution-specific config generation (kind.yaml, k3d.yaml, ksail.yaml)
//   - Kustomization file generation for GitOps workflows
//
// The Scaffolder struct manages generators for different configuration types and
// handles the orchestration of file generation with force/overwrite logic.
package scaffolder

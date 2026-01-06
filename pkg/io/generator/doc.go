// Package generator provides an interface for generating files from code.
//
// This package defines the Generator interface implemented by specific
// distribution generators for generating configuration files from Go structs.
//
// Key functionality:
//   - Generator[T, Options]: Generic interface for content generation
//   - Generate: Transform model into string representation
//
// Subpackages:
//   - argocd: ArgoCD configuration generator
//   - flux: Flux configuration generator
//   - k3d: K3d YAML configuration generator
//   - kind: Kind YAML configuration generator
//   - kustomization: Kustomization YAML generator
//   - talos: Talos configuration generator
//   - yaml: Generic YAML generator using reflection
package generator

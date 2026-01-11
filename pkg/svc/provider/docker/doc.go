// Package docker provides a Docker-based infrastructure provider.
//
// The Docker provider manages Kubernetes cluster nodes as Docker containers.
// It supports different container labeling schemes for different distributions:
//   - Kind: Uses container name prefix "kind-"
//   - K3d: Uses container labels "k3d.cluster" and "k3d.role"
//   - Talos: Uses container labels "talos.cluster.name" and "talos.type"
//
// This provider is used by provisioners to perform infrastructure operations
// while the provisioners handle distribution-specific configuration.
package docker

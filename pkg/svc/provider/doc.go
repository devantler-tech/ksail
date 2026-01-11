// Package provider defines infrastructure providers for running Kubernetes cluster nodes.
//
// Providers handle infrastructure-level operations:
//   - Creating and destroying nodes (Docker containers, cloud VMs, etc.)
//   - Starting and stopping nodes
//   - Managing provider-specific resources (networks, volumes, port mappings)
//
// This package is separate from the provisioner package which handles
// distribution-specific operations (bootstrapping K8s, configuring etcd, etc.).
//
// Currently supported providers:
//   - Docker: Runs cluster nodes as Docker containers (for Kind, K3d, Talos)
package provider

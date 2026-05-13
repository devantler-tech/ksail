// Package kubernetes provides a Kubernetes-based infrastructure provider.
//
// The Kubernetes provider manages nested cluster nodes as pods inside an existing
// host Kubernetes cluster. It supports two execution modes:
//   - Direct pod: K3s runs directly in a privileged pod (k3k-style)
//   - DinD (Docker-in-Docker): Kind, Talos, and VCluster run via a Docker daemon sidecar
//
// Nested cluster API servers are exposed via Gateway API TCPRoute resources,
// and each cluster gets an isolated namespace (ksail-<cluster-name>).
package kubernetes

// Package kubernetes provides a Kubernetes-based infrastructure provider.
//
// The Kubernetes provider manages nested cluster nodes as pods inside an existing
// host Kubernetes cluster. It supports two execution modes:
//   - Direct pod: K3s (via kwokctl/KWOK) runs directly in a privileged DinD pod
//   - DinD (Docker-in-Docker): Kind and Talos run via a Docker daemon sidecar
//   - Helm release: VCluster is deployed via the vCluster Helm driver
//
// Nested cluster API servers are exposed via Gateway API TCPRoute resources.
// Each cluster gets an isolated namespace on the host cluster; the namespace
// prefix depends on the distribution: "ksail-<name>" for Kind, K3s, and Talos,
// and "vcluster-<name>" for VCluster.
package kubernetes

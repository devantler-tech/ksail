// Package kubernetes provides a Kubernetes-based infrastructure provider.
//
// The Kubernetes provider manages nested cluster nodes as pods inside an existing
// host Kubernetes cluster. It supports execution modes by distribution:
//   - K3s: Direct pod execution via k3k operator; K3s binary runs directly in a privileged pod
//   - Kind/Talos: Docker-in-Docker (DinD) with Docker daemon sidecar
//   - VCluster: Helm release deployed via the vCluster Helm driver
//
// Nested cluster API servers are exposed via Gateway API TCPRoute resources.
// Each cluster gets an isolated namespace on the host cluster; the namespace
// prefix depends on the distribution: "k3k-<name>" for K3s, "ksail-<name>" for
// Kind and Talos, and "vcluster-<name>" for VCluster.
package kubernetes

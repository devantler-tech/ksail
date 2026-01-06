// Package provisioner provides cluster and registry provisioning services.
//
// This package contains subpackages for managing Kubernetes cluster
// and container registry lifecycles:
//
//   - cluster: Cluster provisioning for Kind, K3d, and Talos distributions
//   - registry: Local OCI registry management
//
// The provisioners handle the full lifecycle including create, start,
// stop, and delete operations with distribution-specific implementations.
package provisioner

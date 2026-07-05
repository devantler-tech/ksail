// Package aksprovisioner manages Azure Kubernetes Service clusters through
// the native Go SDK (pkg/client/aks), mirroring the GKE provisioner's shape:
// cluster lifecycle (Create/Delete/List/Exists) delegates to the AKS client,
// while Start/Stop delegate to the Azure infrastructure provider
// (pkg/svc/provider/azure). Unlike GKE, AKS exposes a native managed-cluster
// stop, so Stop deallocates the whole cluster — control plane included —
// rather than scaling node pools to zero.
package aksprovisioner

// Package azure implements provider.Provider for Azure Kubernetes Service over
// the pkg/client/aks cluster-lifecycle client. AKS agent pools are not
// individual nodes — like GKE node pools and EKS nodegroups they are managed
// groups — so the provider collapses each pool to a single NodeInfo, and node
// start/stop is implemented as pool resizes (the control plane is
// Azure-managed and keeps running either way). It is the AKS sibling of the
// gcp provider and mirrors its lifecycle semantics.
package azure

// Package azure implements provider.Provider for Azure Kubernetes Service over
// the pkg/client/aks cluster-lifecycle client. AKS agent pools are not
// individual nodes — like GKE node pools and EKS nodegroups they are managed
// groups — so the provider collapses each pool to a single NodeInfo. Node
// start/stop uses AKS's native cluster stop/start (system agent pools cannot
// be resized to zero, so the gcp provider's pool-resize stop does not
// translate): the whole cluster — control plane included — deallocates and
// resumes with its state kept. It is the AKS sibling of the gcp provider.
package azure

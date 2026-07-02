// Package gcp implements provider.Provider for Google Kubernetes Engine over
// the pkg/client/gke cluster-lifecycle client. GKE node pools are not
// individual nodes — like EKS nodegroups they are managed groups — so the
// provider collapses each pool to a single NodeInfo, and node start/stop is
// implemented as pool resizes (the control plane is Google-managed and keeps
// running either way).
package gcp

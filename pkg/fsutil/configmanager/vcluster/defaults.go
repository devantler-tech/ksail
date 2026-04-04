package vcluster

// DefaultKubernetesVersion is the default Kubernetes version for vCluster nodes.
// This value is read from the Dockerfile in this package which must be updated manually
// (Dependabot cannot track these images; see dependabot-core#13383).
//
//nolint:gochecknoglobals // Exported constant initialized from embedded Dockerfile
var DefaultKubernetesVersion = kubernetesVersion()

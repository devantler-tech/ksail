package vcluster

// DefaultKubernetesVersion is the default Kubernetes version for vCluster nodes.
// This value is read from the Dockerfile in this package. When Dependabot does not
// track the referenced images (see dependabot-core#13383), the Dockerfile must be
// updated manually to keep this default in sync.
//
//nolint:gochecknoglobals // Exported constant initialized from embedded Dockerfile
var DefaultKubernetesVersion = kubernetesVersion()

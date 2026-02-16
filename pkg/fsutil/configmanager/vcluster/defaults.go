package vcluster

// DefaultKubernetesVersion is the default Kubernetes version for vCluster nodes.
// This value is read from the Dockerfile in this package which is updated by Dependabot.
//
// v1.35.0: ghcr.io/loft-sh/kubernetes:v1.35.0-full ships a corrupt kine binary.
// v1.34.0 and v1.33.0: kubelet config includes imagePullCredentialsVerificationPolicy
// without the required KubeletEnsureSecretPulledImages feature gate.
// v1.32.3 is the latest version that works correctly.
// Remove this override comment once the upstream images are fixed.
//
//nolint:gochecknoglobals // Exported constant initialized from embedded Dockerfile
var DefaultKubernetesVersion = kubernetesVersion()

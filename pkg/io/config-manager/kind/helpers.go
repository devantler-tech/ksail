package kind

import (
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// DefaultClusterName is the default cluster name for Kind clusters.
const DefaultClusterName = "kind"

// DefaultNetworkName is the Docker network name used by Kind clusters.
const DefaultNetworkName = "kind"

// DefaultMirrorsDir is the default directory name for Kind containerd host mirror configuration.
const DefaultMirrorsDir = "kind/mirrors"

// ResolveMirrorsDir returns the configured mirrors directory or the default.
// It extracts the mirrors directory from the cluster configuration if set,
// otherwise returns DefaultMirrorsDir.
func ResolveMirrorsDir(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Kind.MirrorsDir != "" {
		return clusterCfg.Spec.Cluster.Kind.MirrorsDir
	}

	return DefaultMirrorsDir
}

// ResolveClusterName returns the effective cluster name from Kind config or cluster config.
// Priority: kindConfig.Name > clusterCfg.Spec.Cluster.Connection.Context > "kind" (default).
// Returns "kind" if both configs are nil or have empty names.
func ResolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
) string {
	if kindConfig != nil {
		if name := strings.TrimSpace(kindConfig.Name); name != "" {
			return name
		}
	}

	if clusterCfg != nil {
		if name := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}
	}

	return DefaultClusterName
}

// KubeletCertRotationPatch is a kubeadm patch to enable kubelet serving certificate rotation.
// This allows the kubelet to request a proper TLS certificate via CSR, which kubelet-csr-approver
// will then approve, enabling secure TLS communication with metrics-server.
const KubeletCertRotationPatch = `kind: KubeletConfiguration
apiVersion: kubelet.config.k8s.io/v1beta1
serverTLSBootstrap: true`

// ApplyKubeletCertRotationPatches adds kubeadm patches to all nodes to enable kubelet cert rotation.
// This is required for secure TLS communication between metrics-server and kubelets.
func ApplyKubeletCertRotationPatches(kindConfig *kindv1alpha4.Cluster) {
	// Ensure at least one node exists
	if len(kindConfig.Nodes) == 0 {
		kindConfig.Nodes = []kindv1alpha4.Node{{Role: kindv1alpha4.ControlPlaneRole}}
	}

	// Add the kubelet cert rotation patch to all nodes
	for i := range kindConfig.Nodes {
		kindConfig.Nodes[i].KubeadmConfigPatches = append(
			kindConfig.Nodes[i].KubeadmConfigPatches,
			KubeletCertRotationPatch,
		)
	}
}

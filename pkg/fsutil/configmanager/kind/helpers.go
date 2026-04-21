package kind

import (
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
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
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Vanilla.MirrorsDir != "" {
		return clusterCfg.Spec.Cluster.Vanilla.MirrorsDir
	}

	return DefaultMirrorsDir
}

// ResolveClusterName returns the effective cluster name from Kind config or cluster config.
// Priority: kindConfig.Name > clusterCfg.Spec.Cluster.Connection.Context > "kind" (default).
// When using Connection.Context, strips the "kind-" prefix that ContextName adds,
// making the ContextName/ResolveClusterName mapping bidirectional
// (matching Talos's "admin@" prefix stripping pattern).
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
		if ctx := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); ctx != "" {
			// Strip the "kind-" prefix added by Distribution.ContextName()
			// to recover the original cluster name.
			if clusterName, ok := strings.CutPrefix(ctx, "kind-"); ok && clusterName != "" {
				return clusterName
			}

			return ctx
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
		kindConfig.Nodes = []kindv1alpha4.Node{{
			Role:  kindv1alpha4.ControlPlaneRole,
			Image: DefaultKindNodeImage,
		}}
	}

	// Add the kubelet cert rotation patch to all nodes
	for i := range kindConfig.Nodes {
		kindConfig.Nodes[i].KubeadmConfigPatches = append(
			kindConfig.Nodes[i].KubeadmConfigPatches,
			KubeletCertRotationPatch,
		)
	}
}

// ImageVerificationPatch is a TOML containerd config patch that enables the image verifier plugin.
// This requires containerd 2.x (Kind v0.31.0+ / kindest/node:v1.35.1+).
// Verifier binaries (e.g., Cosign, Notation) must be pre-installed in the Kind node image
// at the configured bin_dir path.
// See: https://github.com/containerd/containerd/blob/main/docs/image-verification.md
const ImageVerificationPatch = `# Enable the containerd image verifier plugin (requires containerd 2.x).
# Verifier binaries must be pre-installed in the Kind node image at bin_dir.
# If bin_dir is empty or missing, image pulls proceed without verification.
# See: https://github.com/containerd/containerd/blob/main/docs/image-verification.md
[plugins."io.containerd.image-verifier.v1.bindir"]
bin_dir = "/opt/image-verifier/bin"
max_verifiers = 10
per_verifier_timeout = "10s"

# --- Example: Cosign keyless verification (OIDC / Sigstore) ---
# Install the cosign verifier binary into /opt/image-verifier/bin/ in a custom Kind node image.
# Cosign will verify signatures using the Sigstore public good instance by default.
# See: https://docs.sigstore.dev/cosign/

# --- Example: Notation verification ---
# Install the notation verifier binary into /opt/image-verifier/bin/ in a custom Kind node image.
# Configure trust policies and certificates in the notation config directory.
# See: https://notaryproject.dev/docs/`

// CDIPatch is a containerd config TOML merge patch that enables CDI (Container Device Interface).
// CDI provides a standardized mechanism for container runtimes to create containers which are able
// to interact with third party devices (e.g., GPUs, network devices, FPGAs).
const CDIPatch = `[plugins."io.containerd.grpc.v1.cri"]
  enable_cdi = true`

// ApplyCDIPatches adds a containerd config patch to enable CDI.
// The patch is applied at the cluster level and affects every node's containerd configuration.
// This function is idempotent — it skips appending if the patch is already present.
func ApplyCDIPatches(kindConfig *kindv1alpha4.Cluster) {
	for _, patch := range kindConfig.ContainerdConfigPatches {
		if strings.Contains(patch, `enable_cdi`) {
			return
		}
	}

	kindConfig.ContainerdConfigPatches = append(
		kindConfig.ContainerdConfigPatches,
		CDIPatch,
	)
}

// ApplyImageVerificationPatches adds a containerd config patch to enable the image verifier plugin.
// The patch is applied at the cluster level and affects every node's containerd configuration.
// This function is idempotent — it skips appending if the patch is already present.
func ApplyImageVerificationPatches(kindConfig *kindv1alpha4.Cluster) {
	for _, patch := range kindConfig.ContainerdConfigPatches {
		if strings.Contains(patch, `io.containerd.image-verifier.v1.bindir`) {
			return
		}
	}

	kindConfig.ContainerdConfigPatches = append(
		kindConfig.ContainerdConfigPatches,
		ImageVerificationPatch,
	)
}

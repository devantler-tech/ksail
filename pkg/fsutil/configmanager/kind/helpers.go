package kind

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/imageverifier"
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
//
//nolint:gochecknoglobals // computed once from imageverifier.BindirPatch; immutable, constant-like value
var ImageVerificationPatch = `# Enable the containerd image verifier plugin (requires containerd 2.x).
# Verifier binaries must be pre-installed in the Kind node image at bin_dir.
# If bin_dir is empty or missing, image pulls proceed without verification.
# See: https://github.com/containerd/containerd/blob/main/docs/image-verification.md
` + imageverifier.BindirPatch(
	"Kind",
)

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

// APIServerMutatingAdmissionPolicyFeatureGate is the feature gate enabling the
// MutatingAdmissionPolicy admission feature.
const APIServerMutatingAdmissionPolicyFeatureGate = "MutatingAdmissionPolicy"

// APIServerAdmissionregistrationV1beta1RuntimeConfig is the runtime-config key that
// serves the admissionregistration.k8s.io/v1beta1 API (which carries
// MutatingAdmissionPolicy / MutatingAdmissionPolicyBinding).
const APIServerAdmissionregistrationV1beta1RuntimeConfig = "admissionregistration.k8s.io/v1beta1"

// ApplyAPIServerFeatureGates enables the MutatingAdmissionPolicy feature gate and the
// admissionregistration.k8s.io/v1beta1 API on the kube-apiserver using Kind's native
// featureGates / runtimeConfig fields. Kind translates these into the kube-apiserver
// --feature-gates / --runtime-config flags (KubeadmConfigPatches for apiServer.extraArgs
// did not reliably reach the apiserver). Calico v3.30+ ships MutatingAdmissionPolicy
// resources in its CRD chart that require this API. Idempotent.
func ApplyAPIServerFeatureGates(kindConfig *kindv1alpha4.Cluster) {
	if kindConfig.FeatureGates == nil {
		kindConfig.FeatureGates = map[string]bool{}
	}

	kindConfig.FeatureGates[APIServerMutatingAdmissionPolicyFeatureGate] = true

	if kindConfig.RuntimeConfig == nil {
		kindConfig.RuntimeConfig = map[string]string{}
	}

	kindConfig.RuntimeConfig[APIServerAdmissionregistrationV1beta1RuntimeConfig] = "true"
}

// ApplyOIDCPatches adds kubeadm config patches to configure the API server with OIDC flags.
// The patch is applied to all control-plane nodes via KubeadmConfigPatches.
// When a CA file is configured, an extraMount is added to make the host CA file
// available inside the Kind container at OIDCCAContainerPath.
func ApplyOIDCPatches(kindConfig *kindv1alpha4.Cluster, oidc *v1alpha1.OIDCSpec) error {
	if oidc == nil || !oidc.Enabled() {
		return nil
	}

	// Ensure at least one node exists
	if len(kindConfig.Nodes) == 0 {
		kindConfig.Nodes = []kindv1alpha4.Node{{
			Role:  kindv1alpha4.ControlPlaneRole,
			Image: DefaultKindNodeImage,
		}}
	}

	patch := buildOIDCKubeadmPatch(oidc)

	for i := range kindConfig.Nodes {
		if kindConfig.Nodes[i].Role == kindv1alpha4.ControlPlaneRole {
			kindConfig.Nodes[i].KubeadmConfigPatches = append(
				kindConfig.Nodes[i].KubeadmConfigPatches,
				patch,
			)
		}
	}

	// Mount the host CA file into all nodes so the API server can read it
	if oidc.CAFile != "" {
		canonicalCAPath, err := fsutil.EvalCanonicalPath(oidc.CAFile)
		if err != nil {
			return fmt.Errorf("failed to resolve OIDC CA file path: %w", err)
		}

		mount := kindv1alpha4.Mount{
			HostPath:      canonicalCAPath,
			ContainerPath: v1alpha1.OIDCCAContainerPath,
			Readonly:      true,
		}

		for i := range kindConfig.Nodes {
			kindConfig.Nodes[i].ExtraMounts = append(kindConfig.Nodes[i].ExtraMounts, mount)
		}
	}

	return nil
}

// buildOIDCKubeadmPatch generates a kubeadm ClusterConfiguration patch with API server OIDC flags.
// kubeadm v1beta4 apiServer.extraArgs is a list of {name, value} entries (not a map); the map form
// is silently ignored, so the flags must be emitted as list items for kubeadm to apply them.
func buildOIDCKubeadmPatch(oidc *v1alpha1.OIDCSpec) string {
	type arg struct {
		name  string
		value string
	}

	args := []arg{
		{name: "oidc-issuer-url", value: oidc.IssuerURL},
		{name: "oidc-client-id", value: oidc.ClientID},
	}

	if oidc.UsernameClaim != "" {
		args = append(args, arg{name: "oidc-username-claim", value: oidc.UsernameClaim})
	}

	if oidc.UsernamePrefix != "" {
		args = append(args, arg{name: "oidc-username-prefix", value: oidc.UsernamePrefix})
	}

	if oidc.GroupsClaim != "" {
		args = append(args, arg{name: "oidc-groups-claim", value: oidc.GroupsClaim})
	}

	if oidc.GroupsPrefix != "" {
		args = append(args, arg{name: "oidc-groups-prefix", value: oidc.GroupsPrefix})
	}

	if oidc.CAFile != "" {
		args = append(args, arg{name: "oidc-ca-file", value: v1alpha1.OIDCCAContainerPath})
	}

	var builder strings.Builder

	_, _ = fmt.Fprintf(&builder, "apiVersion: kubeadm.k8s.io/v1beta4\n")
	_, _ = fmt.Fprintf(&builder, "kind: ClusterConfiguration\n")
	_, _ = fmt.Fprintf(&builder, "apiServer:\n")
	_, _ = fmt.Fprintf(&builder, "  extraArgs:\n")

	for _, a := range args {
		_, _ = fmt.Fprintf(&builder, "    - name: %s\n", a.name)
		_, _ = fmt.Fprintf(&builder, "      value: %q\n", a.value)
	}

	return builder.String()
}

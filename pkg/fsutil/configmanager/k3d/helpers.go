package k3d

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/imageverifier"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/yaml"
)

// mirrorConfigEntry represents a single mirror registry configuration entry in K3d.
type mirrorConfigEntry struct {
	Endpoint []string `yaml:"endpoint"`
}

// mirrorConfig represents the mirrors section of K3d's registry configuration.
type mirrorConfig struct {
	Mirrors map[string]mirrorConfigEntry `yaml:"mirrors"`
}

// ParseRegistryConfig parses K3d registry mirror configuration from raw YAML string.
// Returns a map of host to endpoints, filtering out empty entries.
// Intentionally returns an empty map (instead of an error) for invalid YAML to support
// graceful degradation when registry configuration is malformed or missing.
func ParseRegistryConfig(raw string) map[string][]string {
	result := make(map[string][]string)

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return result
	}

	var cfg mirrorConfig

	err := yaml.Unmarshal([]byte(trimmed), &cfg)
	if err != nil {
		return result
	}

	for host, entry := range cfg.Mirrors {
		if len(entry.Endpoint) == 0 {
			continue
		}

		filtered := make([]string, 0, len(entry.Endpoint))
		for _, endpoint := range entry.Endpoint {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" {
				continue
			}

			filtered = append(filtered, endpoint)
		}

		if len(filtered) == 0 {
			continue
		}

		result[host] = filtered
	}

	return result
}

// ResolveClusterName returns the effective cluster name from K3d config or cluster config.
// Priority: k3dConfig.Name > clusterCfg.Spec.Cluster.Connection.Context > DefaultClusterName.
// Returns DefaultClusterName if both configs are nil or have empty names.
func ResolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
) string {
	if k3dConfig != nil {
		if name := strings.TrimSpace(k3dConfig.Name); name != "" {
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

// ContainerdConfigTemplatePath is the path inside K3d node containers where K3s
// looks for a custom containerd config template. When this file exists, K3s uses
// it instead of its built-in default to generate the final containerd config.
const ContainerdConfigTemplatePath = "/var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl"

// DefaultImageVerifierDir is the default directory for the K3d containerd config template
// relative to the project root. The template file is generated during `ksail project init`
// when image verification is enabled for the K3s distribution.
const DefaultImageVerifierDir = "k3d/containerd"

// ImageVerificationConfigTemplate is a K3s containerd config template (Go template)
// that enables the image verifier plugin. K3s processes this template to generate
// the final containerd config.toml. The template includes K3s's essential Go template
// markers ({{ .PrivateRegistryConfig }}) so K3s can inject its dynamic configuration
// (e.g., registry mirrors configured via K3d's registries.yaml).
//
// The image verifier plugin section configures containerd's bindir verifier, which
// invokes verifier binaries (e.g., Cosign, Notation) from bin_dir before pulling images.
// If bin_dir is empty or contains no verifier binaries, image pulls proceed without
// verification (containerd's default behavior).
//
// Requires K3s v1.35+ (containerd 2.2+).
// See: https://github.com/containerd/containerd/blob/main/docs/image-verification.md
//
//nolint:gochecknoglobals // computed once from imageverifier.BindirPatch; immutable, constant-like value
var ImageVerificationConfigTemplate = `# K3s containerd config template (containerd 2.x, config v3)
# This file is processed by K3s as a Go template to generate the final containerd config.
# It replaces K3s's built-in default template. K3s template markers (e.g.,
# {{ "{{ .PrivateRegistryConfig }}" }}) are preserved so K3s can inject dynamic settings.
#
# See: https://docs.k3s.io/advanced#configuring-containerd
version = 3

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  SystemdCgroup = true

# K3s injects private registry mirror configuration here at runtime.
# Do not remove this template marker.
{{ .PrivateRegistryConfig }}

# --- Image Verifier Plugin (containerd 2.2+) ---
# Enable the containerd image verifier plugin. Verifier binaries must be
# pre-installed in the K3s node image at the configured bin_dir path.
# If bin_dir is empty or missing, image pulls proceed without verification.
# See: https://github.com/containerd/containerd/blob/main/docs/image-verification.md
` + imageverifier.BindirPatch("K3s") + "\n"

// ApplyImageVerificationVolumes adds a volume mount to the K3d config to inject
// the containerd config template into K3d node containers. The template is mounted
// at K3s's custom containerd config template path so K3s uses it to generate the
// final containerd config with the image verifier plugin enabled.
//
// This function is idempotent — it skips appending if the volume mount is already present.
func ApplyImageVerificationVolumes(
	k3dConfig *v1alpha5.SimpleConfig,
	templateHostPath string,
) {
	// Build the volume mount spec in Docker volume format: "host:container"
	volumeSpec := templateHostPath + ":" + ContainerdConfigTemplatePath

	// Check if a volume targeting the container path already exists.
	// If found, update the host path to match the desired template path (handles
	// project directory moves) and return.
	for i, vol := range k3dConfig.Volumes {
		if strings.Contains(vol.Volume, ContainerdConfigTemplatePath) {
			k3dConfig.Volumes[i].Volume = volumeSpec

			return
		}
	}

	k3dConfig.Volumes = append(k3dConfig.Volumes, v1alpha5.VolumeWithNodeFilters{
		Volume:      volumeSpec,
		NodeFilters: []string{"all"},
	})
}

// ApplyOIDCCAVolume adds a volume mount to the K3d config to inject the host
// OIDC CA certificate into K3d node containers. The CA file is mounted at
// OIDCCAContainerPath so the API server can reference it via --oidc-ca-file.
//
// This function is idempotent — it skips appending if the volume mount is already present.
func ApplyOIDCCAVolume(k3dConfig *v1alpha5.SimpleConfig, hostCAPath string) error {
	canonicalCAPath, err := fsutil.EvalCanonicalPath(hostCAPath)
	if err != nil {
		return fmt.Errorf("failed to resolve OIDC CA file path: %w", err)
	}

	volumeSpec := canonicalCAPath + ":" + v1alpha1.OIDCCAContainerPath + ":ro"

	for i, vol := range k3dConfig.Volumes {
		if strings.Contains(vol.Volume, v1alpha1.OIDCCAContainerPath) {
			k3dConfig.Volumes[i].Volume = volumeSpec

			return nil
		}
	}

	k3dConfig.Volumes = append(k3dConfig.Volumes, v1alpha5.VolumeWithNodeFilters{
		Volume:      volumeSpec,
		NodeFilters: []string{"server:*"},
	})

	return nil
}

// APIServerFeatureGatesArgs returns the K3s kube-apiserver args that enable the
// MutatingAdmissionPolicy feature gate and the admissionregistration.k8s.io/v1beta1
// API. Calico v3.30+'s CRD chart ships MutatingAdmissionPolicy / MutatingAdmissionPolicyBinding
// resources that require this API to be served. The args use the K3s
// "--kube-apiserver-arg=..." form, suitable for the k3k Cluster spec's serverArgs.
func APIServerFeatureGatesArgs() []string {
	return []string{
		"--kube-apiserver-arg=feature-gates=MutatingAdmissionPolicy=true",
		"--kube-apiserver-arg=runtime-config=admissionregistration.k8s.io/v1beta1=true",
	}
}

// APIServerFeatureGatesArgsForCNI returns the K3s kube-apiserver args required by the
// given CNI, or nil when the CNI needs none. Only Calico (v3.30+, whose CRD chart ships
// MutatingAdmissionPolicy resources) requires the MutatingAdmissionPolicy feature gate /
// v1beta1 admissionregistration API.
func APIServerFeatureGatesArgsForCNI(cni v1alpha1.CNI) []string {
	if cni == v1alpha1.CNICalico {
		return APIServerFeatureGatesArgs()
	}

	return nil
}

// ResolveNetworkName returns the Docker network name for a K3d cluster.
// K3d uses "k3d-<clustername>" as the network naming convention.
func ResolveNetworkName(clusterName string) string {
	trimmed := strings.TrimSpace(clusterName)
	if trimmed == "" {
		return "k3d"
	}

	return "k3d-" + trimmed
}

// ApplyAPIServerFeatureGatesArgs appends K3s kube-apiserver args enabling the
// MutatingAdmissionPolicy feature gate / v1beta1 admissionregistration API to the
// server nodes, required by Calico v3.30+'s CRD chart (which ships
// MutatingAdmissionPolicy resources). It is idempotent — args already present are
// not re-added.
func ApplyAPIServerFeatureGatesArgs(k3dConfig *v1alpha5.SimpleConfig) {
	for _, arg := range APIServerFeatureGatesArgs() {
		if k3sArgPresent(k3dConfig.Options.K3sOptions.ExtraArgs, arg) {
			continue
		}

		k3dConfig.Options.K3sOptions.ExtraArgs = append(
			k3dConfig.Options.K3sOptions.ExtraArgs,
			v1alpha5.K3sArgWithNodeFilters{
				Arg:         arg,
				NodeFilters: []string{"server:*"},
			},
		)
	}
}

// k3sArgPresent reports whether the K3s extra args already include the given arg.
func k3sArgPresent(existing []v1alpha5.K3sArgWithNodeFilters, arg string) bool {
	for _, entry := range existing {
		if entry.Arg == arg {
			return true
		}
	}

	return false
}

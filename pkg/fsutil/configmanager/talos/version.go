package talos

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/compatibility"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
)

// ResolveClusterKubernetesVersion determines the Kubernetes version for a
// cluster while accounting for provider-specific Talos bootstrap constraints.
// Hetzner's tracked default ISO can lag the installed Talos target, so generated
// configuration must remain valid for that maintenance environment.
func ResolveClusterKubernetesVersion(cluster *v1alpha1.Cluster) string {
	return ResolveKubernetesVersion(
		ResolveClusterTalosCompatibilityVersion(cluster),
		cluster.Spec.Cluster.KubernetesVersion,
	)
}

// ResolveClusterTalosCompatibilityVersion returns the Talos version that
// constrains generated Kubernetes configuration. For Hetzner's default ISO,
// this is the older of the requested Talos target and the bootstrap ISO's Talos
// release. Other providers and custom Hetzner ISOs retain the requested target.
func ResolveClusterTalosCompatibilityVersion(cluster *v1alpha1.Cluster) string {
	talosVersion := cluster.Spec.Cluster.Talos.Version

	if cluster.Spec.Cluster.Provider == v1alpha1.ProviderHetzner &&
		(cluster.Spec.Cluster.Talos.ISO == 0 ||
			cluster.Spec.Cluster.Talos.ISO == v1alpha1.DefaultTalosISO) {
		talosVersion = olderTalosVersion(
			talosVersion,
			v1alpha1.DefaultHetznerTalosVersion,
		)
	}

	return talosVersion
}

// olderTalosVersion returns the older of two Talos versions. An empty primary
// version means the fallback is the only known compatibility constraint. An
// invalid primary is preserved so the existing version-contract validation can
// report it to the user.
func olderTalosVersion(primary, fallback string) string {
	primary = strings.TrimSpace(primary)
	if primary == "" {
		return fallback
	}

	primaryVersion, err := semver.NewVersion(strings.TrimPrefix(primary, "v"))
	if err != nil {
		return primary
	}

	fallbackVersion, err := semver.NewVersion(strings.TrimPrefix(fallback, "v"))
	if err != nil || primaryVersion.LessThan(fallbackVersion) {
		return primary
	}

	return fallback
}

// normalizeKubernetesVersion trims surrounding whitespace and any leading "v" so
// the value matches the form the Talos bundle expects for KubeVersion ("1.32.0",
// not "v1.32.0"). Returns "" for an empty/whitespace input.
func normalizeKubernetesVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// ParseVersionContract resolves a pinned Talos version to the machinery
// contract used for config generation. An empty pin follows the Talos release
// provided by KSail's tracked default Hetzner bootstrap ISO.
func ParseVersionContract(pinnedVersion string) (*talosconfig.VersionContract, error) {
	pinnedVersion = strings.TrimSpace(pinnedVersion)
	if pinnedVersion == "" {
		pinnedVersion = v1alpha1.DefaultHetznerTalosVersion
	}

	if !strings.HasPrefix(pinnedVersion, "v") {
		pinnedVersion = "v" + pinnedVersion
	}

	contract, err := talosconfig.ParseContractFromVersion(pinnedVersion)
	if err != nil {
		return nil, fmt.Errorf(
			"parse Talos version contract for pinned version %q: %w",
			pinnedVersion,
			err,
		)
	}

	return contract, nil
}

// resolveVersionContract preserves an explicit contract or resolves the tracked
// Hetzner bootstrap ISO contract when none was provided.
func resolveVersionContract(
	versionContract *talosconfig.VersionContract,
) (*talosconfig.VersionContract, error) {
	if versionContract != nil {
		return versionContract, nil
	}

	defaultContract, err := ParseVersionContract("")
	if err != nil {
		return nil, fmt.Errorf("resolve default Hetzner Talos version contract: %w", err)
	}

	return defaultContract, nil
}

// ResolveKubernetesVersion determines the Kubernetes version a freshly generated
// Talos machine config should target, given an optional explicit pin
// (spec.cluster.kubernetesVersion) and an optional pinned Talos OS version
// (spec.cluster.talos.version):
//
//   - An explicit pin is honoured verbatim (normalised to drop any "v" prefix).
//   - Otherwise DefaultKubernetesVersion is used, but capped to the newest
//     Kubernetes version compatible with the pinned Talos release when the default
//     is too new for it (e.g. Talos 1.12 supports Kubernetes <= 1.35, so the 1.36
//     default is capped to 1.35). This keeps a pinned older Talos version from
//     being paired with a Kubernetes version it cannot run.
//   - When no Talos version is pinned (or it cannot be parsed), the default is
//     returned unchanged.
//
// On an existing cluster the provisioner additionally prefers the running
// Kubernetes version when no explicit pin is set; see the Talos provisioner's
// buildDesiredNodeConfig. ResolveKubernetesVersion only governs the create-time
// baseline.
func ResolveKubernetesVersion(pinnedTalosVersion, pinnedKubernetesVersion string) string {
	if pinned := normalizeKubernetesVersion(pinnedKubernetesVersion); pinned != "" {
		return pinned
	}

	return defaultKubernetesVersionForTalos(pinnedTalosVersion)
}

// defaultKubernetesVersionForTalos returns DefaultKubernetesVersion capped to the
// newest version the pinned Talos release supports. It returns the default
// unchanged when no Talos version is pinned, the Talos version cannot be parsed,
// or the default is already compatible.
func defaultKubernetesVersionForTalos(pinnedTalosVersion string) string {
	pinnedTalosVersion = strings.TrimSpace(pinnedTalosVersion)
	if pinnedTalosVersion == "" {
		return DefaultKubernetesVersion
	}

	if !strings.HasPrefix(pinnedTalosVersion, "v") {
		pinnedTalosVersion = "v" + pinnedTalosVersion
	}

	talosVersion, err := compatibility.ParseTalosVersion(
		&machine.VersionInfo{Tag: pinnedTalosVersion},
	)
	if err != nil {
		return DefaultKubernetesVersion
	}

	return highestCompatibleKubernetesVersion(talosVersion)
}

// highestCompatibleKubernetesVersion walks minor versions down from
// DefaultKubernetesVersion and returns the first (newest) one compatible with the
// given Talos version. Patch levels are pinned to .0 because Talos expresses its
// support window per minor version. Falls back to DefaultKubernetesVersion when no
// compatible version is found, so the caller (or Talos itself) surfaces the
// incompatibility rather than this silently choosing an unrelated version.
func highestCompatibleKubernetesVersion(talosVersion *compatibility.TalosVersion) string {
	defaultVersion, err := compatibility.ParseKubernetesVersion(DefaultKubernetesVersion)
	if err == nil && defaultVersion.SupportedWith(talosVersion) == nil {
		return DefaultKubernetesVersion
	}

	major, minor, ok := splitMajorMinor(DefaultKubernetesVersion)
	if !ok {
		return DefaultKubernetesVersion
	}

	for candidateMinor := minor; candidateMinor >= 0; candidateMinor-- {
		candidate := fmt.Sprintf("%d.%d.0", major, candidateMinor)

		parsed, err := compatibility.ParseKubernetesVersion(candidate)
		if err != nil {
			continue
		}

		if parsed.SupportedWith(talosVersion) == nil {
			return candidate
		}
	}

	return DefaultKubernetesVersion
}

// splitMajorMinor parses "major.minor[.patch]" into its numeric major and minor
// components. Returns ok=false when the string is not in that form.
func splitMajorMinor(version string) (int, int, bool) {
	majorStr, rest, ok := strings.Cut(version, ".")
	if !ok {
		return 0, 0, false
	}

	minorStr, _, _ := strings.Cut(rest, ".") // patch (if any) is ignored

	major, err := strconv.Atoi(majorStr)
	if err != nil {
		return 0, 0, false
	}

	minor, err := strconv.Atoi(minorStr)
	if err != nil {
		return 0, 0, false
	}

	return major, minor, true
}

// KubernetesVersionFromProvider reports the Kubernetes version a running Talos
// machine config targets, read from the kube-apiserver image tag (falling back to
// the kubelet image for worker configs that carry no API server section). The
// result is normalised to drop any "v" prefix; it returns "" when neither image
// carries a usable tag.
func KubernetesVersionFromProvider(provider talosconfig.Provider) string {
	if provider == nil {
		return ""
	}

	// Talos alpha.2 moved the kube-apiserver settings from cluster.apiServer to the
	// K8sAPIServerConfig document; read the image tag from there. The bridge returns a
	// non-nil config even for worker configs (defaulting the image), so the empty-tag
	// check is what drives the kubelet fallback below, as before.
	if apiServer := provider.K8sAPIServerConfig(); apiServer != nil {
		if tag := extractImageTag(apiServer.Image()); tag != "" {
			return normalizeKubernetesVersion(tag)
		}
	}

	if machineCfg := provider.Machine(); machineCfg != nil {
		if kubelet := machineCfg.Kubelet(); kubelet != nil {
			if tag := extractImageTag(kubelet.Image()); tag != "" {
				return normalizeKubernetesVersion(tag)
			}
		}
	}

	return ""
}

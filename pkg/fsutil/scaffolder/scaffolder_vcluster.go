package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	vclusterconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/vcluster"
)

// VClusterConfigFile is the default vCluster configuration filename.
const VClusterConfigFile = "vcluster.yaml"

// generateVClusterConfig generates the vcluster.yaml configuration file.
// VCluster uses a plain YAML values file (Helm-style) rather than a typed API struct,
// so scaffolding generates a minimal empty config with a descriptive comment.
func (s *Scaffolder) generateVClusterConfig(output string, force bool) error {
	configPath := filepath.Join(output, VClusterConfigFile)

	skip, existed, previousModTime := s.checkFileExistsAndSkip(
		configPath,
		VClusterConfigFile,
		force,
	)
	if skip {
		return nil
	}

	// Write a YAML values file with the default Kubernetes version override.
	// The version is read from the embedded Dockerfile in the vcluster
	// configmanager package, which is automatically updated by Dependabot.
	// Users can change this version in the generated file.
	header := "# vCluster Helm values configuration.\n" +
		"# See https://www.vcluster.com/docs/configure/vcluster-yaml" +
		" for available options.\n" +
		"controlPlane:\n" +
		"  distro:\n" +
		"    k8s:\n" +
		"      image:\n" +
		fmt.Sprintf("        tag: %s\n", vclusterconfigmanager.DefaultKubernetesVersion)

	// Always emit the API server extraArgs block: the MutatingAdmissionPolicy feature
	// gate / v1beta1 admissionregistration API is required by Calico v3.30+, and OIDC
	// flags are appended when configured. Emitting a single block avoids duplicate
	// apiServer keys in the generated values file.
	header += buildVClusterAPIServerArgs(&s.KSailConfig.Spec.Cluster.OIDC)

	// Add volume mounts to inject the OIDC CA certificate into the VCluster pod
	if s.KSailConfig.Spec.Cluster.OIDC.Enabled() && s.KSailConfig.Spec.Cluster.OIDC.CAFile != "" {
		header += buildVClusterOIDCVolumes()
	}

	content := []byte(header)

	err := os.WriteFile(configPath, content, filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrVClusterConfigGeneration, err)
	}

	if force && existed {
		err = ensureOverwriteModTime(configPath, previousModTime)
		if err != nil {
			return fmt.Errorf("failed to update mod time for %s: %w", VClusterConfigFile, err)
		}
	}

	s.notifyFileAction(VClusterConfigFile, existed)

	return nil
}

// buildVClusterAPIServerArgs generates the controlPlane.distro.k8s.apiServer.extraArgs
// YAML fragment for the VCluster values file. It always enables the
// MutatingAdmissionPolicy feature gate / admissionregistration.k8s.io/v1beta1 API
// (required by Calico v3.30+'s CRD chart) and appends OIDC flags when OIDC is
// configured. Emitting a single apiServer block keeps the generated YAML valid.
// Values are escaped with %q to handle YAML-special characters safely. When a CA file
// is configured, the OIDC CA arg references OIDCCAContainerPath and a hostPath volume
// mount is added (VCluster runs on a Docker-based host cluster where the host CA file
// is already mounted at OIDCCAContainerPath on all nodes).
func buildVClusterAPIServerArgs(oidc *v1alpha1.OIDCSpec) string {
	const (
		featureGatesArg  = "--feature-gates=MutatingAdmissionPolicy=true"
		runtimeConfigArg = "--runtime-config=admissionregistration.k8s.io/v1beta1=true"
	)

	result := "      apiServer:\n" +
		"        extraArgs:\n" +
		fmt.Sprintf("          - %q\n", featureGatesArg) +
		fmt.Sprintf("          - %q\n", runtimeConfigArg)

	if oidc == nil || !oidc.Enabled() {
		return result
	}

	result += fmt.Sprintf("          - %q\n", "--oidc-issuer-url="+oidc.IssuerURL) +
		fmt.Sprintf("          - %q\n", "--oidc-client-id="+oidc.ClientID)

	if oidc.UsernameClaim != "" {
		result += fmt.Sprintf("          - %q\n", "--oidc-username-claim="+oidc.UsernameClaim)
	}

	if oidc.UsernamePrefix != "" {
		result += fmt.Sprintf("          - %q\n", "--oidc-username-prefix="+oidc.UsernamePrefix)
	}

	if oidc.GroupsClaim != "" {
		result += fmt.Sprintf("          - %q\n", "--oidc-groups-claim="+oidc.GroupsClaim)
	}

	if oidc.GroupsPrefix != "" {
		result += fmt.Sprintf("          - %q\n", "--oidc-groups-prefix="+oidc.GroupsPrefix)
	}

	if oidc.CAFile != "" {
		result += fmt.Sprintf("          - %q\n", "--oidc-ca-file="+v1alpha1.OIDCCAContainerPath)
	}

	return result
}

// buildVClusterOIDCVolumes generates the YAML fragment for VCluster volume mounts
// that inject the OIDC CA certificate into the VCluster control plane pod.
// Uses controlPlane.statefulSet.persistence.addVolumeMounts/addVolumes per the
// vCluster Helm values schema. A hostPath volume is used because the host
// Kubernetes node (Kind/K3d) already has the CA file mounted at OIDCCAContainerPath.
func buildVClusterOIDCVolumes() string {
	return "  statefulSet:\n" +
		"    persistence:\n" +
		"      addVolumeMounts:\n" +
		"        - name: oidc-ca\n" +
		"          mountPath: " + v1alpha1.OIDCCAContainerPath + "\n" +
		"          readOnly: true\n" +
		"      addVolumes:\n" +
		"        - name: oidc-ca\n" +
		"          hostPath:\n" +
		"            path: " + v1alpha1.OIDCCAContainerPath + "\n" +
		"            type: File\n"
}

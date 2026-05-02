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

	// Append API server OIDC flags when OIDC is configured
	if s.KSailConfig.Spec.Cluster.OIDC.Enabled() {
		header += buildVClusterOIDCArgs(&s.KSailConfig.Spec.Cluster.OIDC)
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

// buildVClusterOIDCArgs generates the YAML fragment for VCluster API server OIDC extra args.
// Values are escaped with %q to handle YAML-special characters safely.
func buildVClusterOIDCArgs(oidc *v1alpha1.OIDCSpec) string {
	result := "      apiServer:\n" +
		"        extraArgs:\n" +
		fmt.Sprintf("          - %q\n", "--oidc-issuer-url="+oidc.IssuerURL) +
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
		result += fmt.Sprintf("          - %q\n", "--oidc-ca-file="+oidc.CAFile)
	}

	return result
}

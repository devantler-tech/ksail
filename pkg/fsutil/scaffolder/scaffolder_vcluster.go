package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
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
	// The SDK defaults to a Kubernetes version whose upstream image has a
	// broken kine binary. Pin to v1.34.0 which has a working binary.
	// Users can change this version in the generated file.
	const header = "# vCluster Helm values configuration.\n" +
		"# See https://www.vcluster.com/docs/configure/vcluster-yaml" +
		" for available options.\n" +
		"controlPlane:\n" +
		"  distro:\n" +
		"    k8s:\n" +
		"      image:\n" +
		"        tag: v1.34.0\n"

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

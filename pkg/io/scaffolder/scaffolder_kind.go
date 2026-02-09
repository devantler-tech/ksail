package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/kind"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// GetKindMirrorsDir returns the configured mirrors directory or the default.
func (s *Scaffolder) GetKindMirrorsDir() string {
	return kindconfigmanager.ResolveMirrorsDir(&s.KSailConfig)
}

// generateKindConfig generates the kind.yaml configuration file.
func (s *Scaffolder) generateKindConfig(output string, force bool) error {
	kindConfig := s.buildKindConfig(output)

	opts := yamlgenerator.Options{
		Output: filepath.Join(output, KindConfigFile),
		Force:  force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[*v1alpha4.Cluster]{
			Gen:         s.KindGenerator,
			Model:       kindConfig,
			Opts:        opts,
			DisplayName: "kind.yaml",
			Force:       force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrKindConfigGeneration, err)
			},
		},
	)
}

// buildKindConfig creates the Kind cluster configuration object.
// Node counts can be set via --control-planes and --workers CLI flags.
func (s *Scaffolder) buildKindConfig(output string) *v1alpha4.Cluster {
	// Determine cluster name - use explicit ClusterName if set, otherwise default
	clusterName := kindconfigmanager.DefaultClusterName
	if s.ClusterName != "" {
		clusterName = s.ClusterName
	}

	kindConfig := &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			APIVersion: "kind.x-k8s.io/v1alpha4",
			Kind:       "Cluster",
		},
		Name: clusterName,
	}

	// Disable default CNI if using a non-default CNI (Cilium or Calico)
	if s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICilium ||
		s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICalico {
		kindConfig.Networking.DisableDefaultCNI = true
	}

	// Enable kubelet certificate rotation when metrics-server is explicitly enabled.
	// This is required for secure TLS communication between metrics-server and kubelets.
	if s.KSailConfig.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled {
		kindconfigmanager.ApplyKubeletCertRotationPatches(kindConfig)
	}

	// Apply node counts from CLI flags (stored in Talos options)
	s.applyKindNodeCounts(kindConfig)

	s.addMirrorMountsToKindConfig(kindConfig, output)

	return kindConfig
}

// applyKindNodeCounts sets up Kind nodes based on --control-planes and --workers CLI flags.
func (s *Scaffolder) applyKindNodeCounts(kindConfig *v1alpha4.Cluster) {
	controlPlanes := int(s.KSailConfig.Spec.Cluster.Talos.ControlPlanes)
	workers := int(s.KSailConfig.Spec.Cluster.Talos.Workers)

	// Only generate nodes if explicitly configured
	if controlPlanes <= 0 && workers <= 0 {
		return
	}

	// Default to 1 control-plane if workers specified but not control-planes
	if controlPlanes <= 0 {
		controlPlanes = 1
	}

	// Build nodes slice
	nodes := make([]v1alpha4.Node, 0, controlPlanes+workers)

	for range controlPlanes {
		nodes = append(nodes, v1alpha4.Node{
			Role:  v1alpha4.ControlPlaneRole,
			Image: kindconfigmanager.DefaultKindNodeImage,
		})
	}

	for range workers {
		nodes = append(nodes, v1alpha4.Node{
			Role:  v1alpha4.WorkerRole,
			Image: kindconfigmanager.DefaultKindNodeImage,
		})
	}

	kindConfig.Nodes = nodes
}

// addMirrorMountsToKindConfig adds extraMounts for mirror registries to the Kind config.
func (s *Scaffolder) addMirrorMountsToKindConfig(kindConfig *v1alpha4.Cluster, output string) {
	specs := registry.ParseMirrorSpecs(s.MirrorRegistries)
	if len(specs) == 0 {
		return
	}

	kindMirrorsDir := s.GetKindMirrorsDir()
	mirrorsDir := filepath.Join(output, kindMirrorsDir)

	absHostsDir, err := filepath.Abs(mirrorsDir)
	if err != nil {
		absHostsDir = mirrorsDir
	}

	if len(kindConfig.Nodes) == 0 {
		kindConfig.Nodes = []v1alpha4.Node{{
			Role:  v1alpha4.ControlPlaneRole,
			Image: kindconfigmanager.DefaultKindNodeImage,
		}}
	}

	for _, spec := range specs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		mount := v1alpha4.Mount{
			HostPath:      filepath.Join(absHostsDir, host),
			ContainerPath: "/etc/containerd/certs.d/" + host,
			Readonly:      true,
		}

		for i := range kindConfig.Nodes {
			kindConfig.Nodes[i].ExtraMounts = append(kindConfig.Nodes[i].ExtraMounts, mount)
		}
	}
}

// generateKindMirrorsConfig generates hosts.toml files for Kind registry mirrors.
// Each mirror registry specification creates a subdirectory under the configured mirrors directory
// (default: kind/mirrors) with a hosts.toml file that configures containerd to use the specified upstream.
func (s *Scaffolder) generateKindMirrorsConfig(output string, force bool) error {
	if s.KSailConfig.Spec.Cluster.Distribution != v1alpha1.DistributionVanilla {
		return nil
	}

	specs := registry.ParseMirrorSpecs(s.MirrorRegistries)
	if len(specs) == 0 {
		return nil
	}

	kindMirrorsDir := s.GetKindMirrorsDir()
	mirrorsDir := filepath.Join(output, kindMirrorsDir)

	for _, spec := range specs {
		registryDir := filepath.Join(mirrorsDir, spec.Host)
		hostsPath := filepath.Join(registryDir, "hosts.toml")
		displayName := filepath.Join(kindMirrorsDir, spec.Host, "hosts.toml")

		skip, existed, previousModTime := s.checkFileExistsAndSkip(hostsPath, displayName, force)
		if skip {
			continue
		}

		// Create directory structure
		err := os.MkdirAll(registryDir, dirPerm)
		if err != nil {
			return fmt.Errorf("failed to create mirror directory %s: %w", registryDir, err)
		}

		// Generate hosts.toml content
		content := registry.GenerateScaffoldedHostsToml(spec)

		// Write hosts.toml file
		err = os.WriteFile(hostsPath, []byte(content), filePerm)
		if err != nil {
			return fmt.Errorf("failed to write hosts.toml to %s: %w", hostsPath, err)
		}

		if force && existed {
			err = ensureOverwriteModTime(hostsPath, previousModTime)
			if err != nil {
				return fmt.Errorf("failed to update mod time for %s: %w", displayName, err)
			}
		}

		s.notifyFileAction(displayName, existed)
	}

	return nil
}

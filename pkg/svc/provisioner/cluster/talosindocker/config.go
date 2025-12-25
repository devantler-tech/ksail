package talosindockerprovisioner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// Default configuration values for TalosInDocker clusters.
const (
	// DefaultClusterName is the default name for TalosInDocker clusters.
	DefaultClusterName = "talos-default"
	// DefaultPatchesDir is the default directory for Talos patches.
	DefaultPatchesDir = "talos"
	// DefaultTalosImage is the default Talos container image.
	DefaultTalosImage = "ghcr.io/siderolabs/talos:latest"
	// DefaultControlPlaneNodes is the default number of control-plane nodes.
	DefaultControlPlaneNodes = 1
	// DefaultWorkerNodes is the default number of worker nodes.
	DefaultWorkerNodes = 0
	// DefaultNetworkCIDR is the default CIDR for the cluster network.
	DefaultNetworkCIDR = "10.5.0.0/24"
	// DefaultKubernetesVersion is the default Kubernetes version.
	DefaultKubernetesVersion = "1.32.0"
)

// TalosInDockerConfig holds configuration for a Talos-in-Docker cluster.
type TalosInDockerConfig struct {
	// ClusterName is the name of the Talos cluster.
	ClusterName string

	// PatchesDir is the root directory containing Talos patches.
	PatchesDir string

	// ClusterPatchesDir contains patches applied to all nodes.
	ClusterPatchesDir string

	// ControlPlanePatchesDir contains patches for control-plane nodes.
	ControlPlanePatchesDir string

	// WorkerPatchesDir contains patches for worker nodes.
	WorkerPatchesDir string

	// TalosImage is the Talos container image to use.
	TalosImage string

	// ControlPlaneNodes is the number of control-plane nodes.
	ControlPlaneNodes int

	// WorkerNodes is the number of worker nodes.
	WorkerNodes int

	// NetworkCIDR is the CIDR for the cluster network.
	NetworkCIDR string

	// KubernetesVersion is the Kubernetes version to deploy.
	KubernetesVersion string

	// KubeconfigPath is the path to write the kubeconfig.
	KubeconfigPath string

	// TalosconfigPath is the path to write the talosconfig.
	TalosconfigPath string

	// MirrorRegistries contains mirror registry specifications in "host=upstream" format.
	// Example: ["docker.io=https://registry.example.com", "ghcr.io=https://ghcr.example.com"]
	MirrorRegistries []string
}

// NewTalosInDockerConfig creates a new TalosInDockerConfig with default values.
func NewTalosInDockerConfig() *TalosInDockerConfig {
	return &TalosInDockerConfig{
		ClusterName:            DefaultClusterName,
		PatchesDir:             DefaultPatchesDir,
		ClusterPatchesDir:      filepath.Join(DefaultPatchesDir, "cluster"),
		ControlPlanePatchesDir: filepath.Join(DefaultPatchesDir, "control-planes"),
		WorkerPatchesDir:       filepath.Join(DefaultPatchesDir, "workers"),
		TalosImage:             DefaultTalosImage,
		ControlPlaneNodes:      DefaultControlPlaneNodes,
		WorkerNodes:            DefaultWorkerNodes,
		NetworkCIDR:            DefaultNetworkCIDR,
		KubernetesVersion:      DefaultKubernetesVersion,
	}
}

// WithClusterName sets the cluster name.
func (c *TalosInDockerConfig) WithClusterName(name string) *TalosInDockerConfig {
	if name != "" {
		c.ClusterName = name
	}

	return c
}

// WithPatchesDir sets the patches directory and updates subdirectory paths.
func (c *TalosInDockerConfig) WithPatchesDir(dir string) *TalosInDockerConfig {
	if dir != "" {
		c.PatchesDir = dir
		c.ClusterPatchesDir = filepath.Join(dir, "cluster")
		c.ControlPlanePatchesDir = filepath.Join(dir, "control-planes")
		c.WorkerPatchesDir = filepath.Join(dir, "workers")
	}

	return c
}

// WithTalosImage sets the Talos container image.
func (c *TalosInDockerConfig) WithTalosImage(image string) *TalosInDockerConfig {
	if image != "" {
		c.TalosImage = image
	}

	return c
}

// WithControlPlaneNodes sets the number of control-plane nodes.
func (c *TalosInDockerConfig) WithControlPlaneNodes(count int) *TalosInDockerConfig {
	if count > 0 {
		c.ControlPlaneNodes = count
	}

	return c
}

// WithWorkerNodes sets the number of worker nodes.
func (c *TalosInDockerConfig) WithWorkerNodes(count int) *TalosInDockerConfig {
	if count >= 0 {
		c.WorkerNodes = count
	}

	return c
}

// WithKubernetesVersion sets the Kubernetes version to deploy.
func (c *TalosInDockerConfig) WithKubernetesVersion(version string) *TalosInDockerConfig {
	if version != "" {
		c.KubernetesVersion = version
	}

	return c
}

// WithKubeconfigPath sets the kubeconfig output path.
func (c *TalosInDockerConfig) WithKubeconfigPath(path string) *TalosInDockerConfig {
	if path != "" {
		c.KubeconfigPath = path
	}

	return c
}

// WithTalosconfigPath sets the talosconfig output path.
func (c *TalosInDockerConfig) WithTalosconfigPath(path string) *TalosInDockerConfig {
	if path != "" {
		c.TalosconfigPath = path
	}

	return c
}

// WithMirrorRegistries sets the mirror registry specifications.
// Format: ["host=upstream", ...] e.g., ["docker.io=https://registry.example.com"]
func (c *TalosInDockerConfig) WithMirrorRegistries(mirrors []string) *TalosInDockerConfig {
	if len(mirrors) > 0 {
		c.MirrorRegistries = mirrors
	}

	return c
}

// ValidatePatchDirectory validates that patch directories exist and contain valid YAML files.
// Returns a warning if the talos directory doesn't exist (patches are optional),
// or an error if YAML files are invalid.
func (c *TalosInDockerConfig) ValidatePatchDirectory() (string, error) {
	// Check if patches directory exists
	_, statErr := os.Stat(c.PatchesDir)
	if os.IsNotExist(statErr) {
		return "Patch directory '" + c.PatchesDir + "/' not found. " +
			"Create it or run 'ksail cluster init --distribution TalosInDocker'.", nil
	}

	// Validate YAML files in each subdirectory
	subdirs := []string{c.ClusterPatchesDir, c.ControlPlanePatchesDir, c.WorkerPatchesDir}

	for _, dir := range subdirs {
		_, dirStatErr := os.Stat(dir)
		if os.IsNotExist(dirStatErr) {
			continue // Subdirectory doesn't exist, skip
		}

		validateErr := validateYAMLFilesInDir(dir)
		if validateErr != nil {
			return "", validateErr
		}
	}

	return "", nil
}

// validateYAMLFilesInDir checks that all .yaml and .yml files in a directory are valid YAML.
func validateYAMLFilesInDir(dir string) error {
	cleanDir := filepath.Clean(dir)

	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", cleanDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(cleanDir, filepath.Clean(name))

		//nolint:gosec // Path from validated directory
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return fmt.Errorf("failed to read patch file '%s': %w", filePath, readErr)
		}

		var parsed any

		yamlErr := yaml.Unmarshal(content, &parsed)
		if yamlErr != nil {
			return fmt.Errorf("failed to parse patch file '%s': %w", filePath, yamlErr)
		}
	}

	return nil
}

package talos

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v6/pkg/envvar"
	configmanager "github.com/devantler-tech/ksail/v6/pkg/fsutil/configmanager"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"sigs.k8s.io/yaml"
)

// Compile-time interface compliance check.
var _ configmanager.ConfigManager[Configs] = (*ConfigManager)(nil)

// DefaultTalosImage is the default Talos container image.
// This value is read from the Dockerfile in this package which is updated by Dependabot.
// NOTE: This MUST match the Talos pkg/machinery version to ensure
// generated machine configs are compatible with the running container.
//
//nolint:gochecknoglobals // Exported constant initialized from embedded Dockerfile
var DefaultTalosImage = talosImage()

// Default configuration values for Talos clusters.
const (
	// DefaultPatchesDir is the default directory for Talos patches.
	DefaultPatchesDir = "talos"
	// DefaultNetworkCIDR is the default CIDR for the cluster network.
	DefaultNetworkCIDR = "10.5.0.0/24"
	// DefaultKubernetesVersion is the default Kubernetes version.
	DefaultKubernetesVersion = "1.32.0"
	// DefaultClusterName is the default cluster name for Talos clusters.
	DefaultClusterName = "talos-default"
)

// ConfigManager implements configuration management for Talos cluster patches.
// Unlike Kind and K3d which load from a single YAML file, Talos patches are
// loaded from multiple directories and merged into machine configurations.
//
// This implements configmanager.ConfigManager[Configs] interface.
type ConfigManager struct {
	patchesDir        string
	clusterName       string
	kubernetesVersion string
	networkCIDR       string
	config            *Configs
	configLoaded      bool
	// additionalPatches are runtime patches added programmatically.
	additionalPatches []Patch
	// versionContract controls which Talos version-gated config fields are generated.
	// Defaults to TalosVersion1_11 for compatibility with Hetzner bootstrap ISOs.
	versionContract *talosconfig.VersionContract
}

// NewConfigManager creates a new configuration manager for Talos patches.
// Parameters:
//   - patchesDir: root directory containing talos/cluster, talos/control-planes, talos/workers
//   - clusterName: name for the Talos cluster
//   - kubernetesVersion: Kubernetes version to deploy
//   - networkCIDR: network CIDR for the cluster (e.g., "10.5.0.0/24")
func NewConfigManager(
	patchesDir, clusterName, kubernetesVersion, networkCIDR string,
) *ConfigManager {
	if patchesDir == "" {
		patchesDir = DefaultPatchesDir
	}

	if kubernetesVersion == "" {
		kubernetesVersion = DefaultKubernetesVersion
	}

	if networkCIDR == "" {
		networkCIDR = DefaultNetworkCIDR
	}

	return &ConfigManager{
		patchesDir:        patchesDir,
		clusterName:       clusterName,
		kubernetesVersion: kubernetesVersion,
		networkCIDR:       networkCIDR,
		configLoaded:      false,
		versionContract:   talosconfig.TalosVersion1_11,
	}
}

// WithVersionContract sets the Talos version contract used during config generation.
// The version contract controls which version-gated fields are included in the generated
// machine config. Use this to opt into features introduced in newer Talos versions.
//
// The default is TalosVersion1_11, which is safe for Hetzner bootstrap ISOs (currently
// Talos 1.11.2). Version contracts greater than 1.11 generate fields unknown to the
// 1.11.2 machined (e.g. machine.install.grubUseUKICmdline), causing bootstrap failures.
// Set a higher contract only when all bootstrap paths support the required Talos version.
func (m *ConfigManager) WithVersionContract(contract *talosconfig.VersionContract) *ConfigManager {
	m.versionContract = contract
	m.config = nil
	m.configLoaded = false

	return m
}

// WithAdditionalPatches adds runtime patches to be applied after file patches.
// This is useful for programmatic patches like CNI disable or mirror registries.
func (m *ConfigManager) WithAdditionalPatches(patches []Patch) *ConfigManager {
	m.additionalPatches = append(m.additionalPatches, patches...)

	return m
}

// Load loads Talos patches from directories and creates the config bundle.
// Returns the loaded Configs, either freshly loaded or previously cached.
// Timer, Silent, IgnoreConfigFile, and SkipValidation options are not currently used.
func (m *ConfigManager) Load(_ configmanager.LoadOptions) (*Configs, error) {
	// Return cached config if already loaded
	if m.configLoaded {
		return m.config, nil
	}

	// Load patches from directories
	patches, err := LoadPatches(m.patchesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load patches: %w", err)
	}

	// Append additional runtime patches
	patches = append(patches, m.additionalPatches...)

	// Create Configs from patches
	configs, err := newConfigs(
		m.clusterName,
		m.kubernetesVersion,
		m.networkCIDR,
		patches,
		m.versionContract,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos configs: %w", err)
	}

	m.config = configs
	m.configLoaded = true

	return m.config, nil
}

// ValidatePatchDirectory validates that patch directories exist and contain valid YAML files.
// Returns a warning message if the talos directory doesn't exist (patches are optional),
// or an error if YAML files are invalid.
func (m *ConfigManager) ValidatePatchDirectory() (string, error) {
	// Check if patches directory exists
	_, statErr := os.Stat(m.patchesDir)
	if os.IsNotExist(statErr) {
		return "Patch directory '" + m.patchesDir + "/' not found. " +
			"Create it or run 'ksail cluster init --distribution Talos'.", nil
	}

	// Validate YAML files in each subdirectory
	subdirs := []string{
		filepath.Join(m.patchesDir, "cluster"),
		filepath.Join(m.patchesDir, "control-planes"),
		filepath.Join(m.patchesDir, "workers"),
	}

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

// ValidateConfigs performs semantic validation by actually loading patches.
// This catches issues that YAML syntax checking alone misses.
func (m *ConfigManager) ValidateConfigs() (*Configs, error) {
	// First do basic YAML validation
	warning, err := m.ValidatePatchDirectory()
	if err != nil {
		return nil, err
	}

	// If patches directory doesn't exist, that's just a warning
	if warning != "" {
		// Still try to create base config
		return m.Load(configmanager.LoadOptions{})
	}

	// Actually load and apply patches
	configs, err := m.Load(configmanager.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to validate Talos configuration: %w", err)
	}

	return configs, nil
}

// forEachYAMLFile iterates over YAML files in a directory and calls the callback for each.
// This is a shared helper to avoid code duplication between manager and patches.
func forEachYAMLFile(dir string, callback func(filePath string, content []byte) error) error {
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

		content, readErr := os.ReadFile(filePath) //nolint:gosec // Path from validated directory
		if readErr != nil {
			return fmt.Errorf("failed to read file '%s': %w", filePath, readErr)
		}

		// Expand environment variables in file content
		content = envvar.ExpandBytes(content)

		callbackErr := callback(filePath, content)
		if callbackErr != nil {
			return callbackErr
		}
	}

	return nil
}

// validateYAMLFilesInDir checks that all .yaml and .yml files in a directory are valid YAML.
func validateYAMLFilesInDir(dir string) error {
	return forEachYAMLFile(dir, func(filePath string, content []byte) error {
		var parsed any

		yamlErr := yaml.Unmarshal(content, &parsed)
		if yamlErr != nil {
			return fmt.Errorf("failed to parse patch file '%s': %w", filePath, yamlErr)
		}

		return nil
	})
}

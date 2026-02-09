package configmanager

import (
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanagerinterface "github.com/devantler-tech/ksail/v5/pkg/io/configmanager"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/talos"
	talosgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// loadKindConfig loads the Kind distribution configuration if it exists.
// Returns ErrDistributionConfigNotFound if the file doesn't exist.
// Returns error if config loading or validation fails.
func (m *ConfigManager) loadKindConfig() (*kindv1alpha4.Cluster, error) {
	// Check if the file actually exists before trying to load it
	// This prevents validation against default configs during init
	_, err := os.Stat(m.Config.Spec.Cluster.DistributionConfig)
	if os.IsNotExist(err) {
		// File doesn't exist
		return nil, ErrDistributionConfigNotFound
	}

	kindManager := kindconfigmanager.NewConfigManager(m.Config.Spec.Cluster.DistributionConfig)

	config, err := kindManager.Load(configmanagerinterface.LoadOptions{})
	if err != nil {
		// Propagate validation errors
		return nil, fmt.Errorf("failed to load Kind config: %w", err)
	}

	return config, nil
}

// loadK3dConfig loads the K3d distribution configuration if it exists.
// Returns ErrDistributionConfigNotFound if the file doesn't exist.
// Returns error if config loading or validation fails.
func (m *ConfigManager) loadK3dConfig() (*k3dv1alpha5.SimpleConfig, error) {
	// Check if the file actually exists before trying to load it
	// This prevents validation against default configs during init
	_, err := os.Stat(m.Config.Spec.Cluster.DistributionConfig)
	if os.IsNotExist(err) {
		// File doesn't exist
		return nil, ErrDistributionConfigNotFound
	}

	k3dManager := k3dconfigmanager.NewConfigManager(m.Config.Spec.Cluster.DistributionConfig)

	config, err := k3dManager.Load(configmanagerinterface.LoadOptions{})
	if err != nil {
		// Propagate validation errors
		return nil, fmt.Errorf("failed to load K3d config: %w", err)
	}

	return config, nil
}

// loadTalosConfig loads the Talos distribution configuration if the patches directory exists.
// Returns ErrDistributionConfigNotFound if the directory doesn't exist.
// Returns error if config loading or validation fails.
func (m *ConfigManager) loadTalosConfig() (*talosconfigmanager.Configs, error) {
	// For Talos, DistributionConfig points to the patches directory (e.g., "talos")
	patchesDir := m.Config.Spec.Cluster.DistributionConfig
	if patchesDir == "" {
		patchesDir = talosconfigmanager.DefaultPatchesDir
	}

	// Check if the directory exists
	info, err := os.Stat(patchesDir)
	if os.IsNotExist(err) {
		return nil, ErrDistributionConfigNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("failed to stat talos patches directory: %w", err)
	}

	if !info.IsDir() {
		return nil, ErrDistributionConfigNotFound
	}

	// Get cluster name from context or use default.
	// Uses ResolveClusterName helper which handles the "admin@<cluster-name>" pattern.
	clusterName := talosconfigmanager.ResolveClusterName(m.Config, nil)

	talosManager := talosconfigmanager.NewConfigManager(
		patchesDir,
		clusterName,
		"", // Use default Kubernetes version
		"", // Use default network CIDR
	)

	config, err := talosManager.Load(configmanagerinterface.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to load Talos config: %w", err)
	}

	return config, nil
}

// loadAndCacheDistributionConfig loads the distribution-specific configuration based on
// the cluster's distribution type and caches it in the ConfigManager.
// This allows commands to access the distribution config via cfgManager.DistributionConfig.
// If distribution config file doesn't exist, an empty DistributionConfig is created.
func (m *ConfigManager) loadAndCacheDistributionConfig() error {
	m.DistributionConfig = &clusterprovisioner.DistributionConfig{}

	switch m.Config.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return m.cacheKindConfig()
	case v1alpha1.DistributionK3s:
		return m.cacheK3dConfig()
	case v1alpha1.DistributionTalos:
		return m.cacheTalosConfig()
	default:
		return nil
	}
}

func (m *ConfigManager) cacheKindConfig() error {
	kindConfig, err := m.loadKindConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return fmt.Errorf("failed to load Kind distribution config: %w", err)
	}

	if kindConfig == nil {
		// Create a valid default Kind config with required TypeMeta fields
		kindConfig = &kindv1alpha4.Cluster{
			TypeMeta: kindv1alpha4.TypeMeta{
				Kind:       "Cluster",
				APIVersion: "kind.x-k8s.io/v1alpha4",
			},
		}
	}

	m.DistributionConfig.Kind = kindConfig

	return nil
}

func (m *ConfigManager) cacheK3dConfig() error {
	k3dConfig, err := m.loadK3dConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return fmt.Errorf("failed to load K3d distribution config: %w", err)
	}

	if k3dConfig == nil {
		// Create a valid default K3d config with required TypeMeta fields
		k3dConfig = k3dconfigmanager.NewK3dSimpleConfig("", "", "")
	}

	m.DistributionConfig.K3d = k3dConfig

	return nil
}

func (m *ConfigManager) cacheTalosConfig() error {
	talosConfig, err := m.loadTalosConfig()
	if err != nil && !errors.Is(err, ErrDistributionConfigNotFound) {
		return fmt.Errorf("failed to load Talos distribution config: %w", err)
	}

	if talosConfig == nil {
		// Create a valid default Talos config with required bundle.
		// When metrics-server is enabled on Talos, we need to add the kubelet-csr-approver
		// as an extraManifest so it's installed during bootstrap. Without this,
		// metrics-server cannot validate kubelet TLS certificates (missing IP SANs).
		patches := m.getDefaultTalosPatches()

		talosConfig, err = talosconfigmanager.NewDefaultConfigsWithPatches(patches)
		if err != nil {
			return fmt.Errorf("failed to create default Talos config: %w", err)
		}
	}

	m.DistributionConfig.Talos = talosConfig

	return nil
}

// getDefaultTalosPatches returns patches that should be applied to the default Talos config
// based on the current cluster configuration.
func (m *ConfigManager) getDefaultTalosPatches() []talosconfigmanager.Patch {
	var patches []talosconfigmanager.Patch

	// When metrics-server is enabled on Talos, we need two patches:
	// 1. Enable kubelet certificate rotation (rotate-server-certificates: true)
	// 2. Install kubelet-serving-cert-approver via extraManifests to approve the CSRs
	//
	// Note: We use alex1989hu/kubelet-serving-cert-approver for Talos because it provides
	// a single manifest URL suitable for extraManifests during bootstrap. For non-Talos
	// distributions, postfinance/kubelet-csr-approver is used via Helm post-bootstrap.
	//
	// See: https://docs.siderolabs.com/kubernetes-guides/monitoring-and-observability/deploy-metrics-server/
	if m.Config.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled {
		// Patch 1: Enable kubelet certificate rotation
		kubeletCertRotationPatch := talosconfigmanager.Patch{
			Path:  "kubelet-cert-rotation",
			Scope: talosconfigmanager.PatchScopeCluster,
			Content: []byte(`machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: "true"
`),
		}
		patches = append(patches, kubeletCertRotationPatch)

		// Patch 2: Install kubelet-serving-cert-approver during bootstrap
		kubeletCSRApproverPatch := talosconfigmanager.Patch{
			Path:  "kubelet-csr-approver-extramanifest",
			Scope: talosconfigmanager.PatchScopeCluster,
			Content: []byte(`cluster:
  extraManifests:
    - ` + talosgenerator.KubeletServingCertApproverManifestURL + `
`),
		}
		patches = append(patches, kubeletCSRApproverPatch)
	}

	return patches
}

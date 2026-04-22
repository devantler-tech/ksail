package configmanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
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

	// Add external cloud provider patch when using Hetzner provider.
	// This enables CCM to initialize nodes with a providerID and write node labels
	// required by the CSI DaemonSet.
	if m.Config.Spec.Cluster.Provider == v1alpha1.ProviderHetzner {
		talosManager.WithAdditionalPatches(externalCloudProviderPatches())
	}

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
	case v1alpha1.DistributionVCluster:
		return m.cacheVClusterConfig()
	case v1alpha1.DistributionKWOK:
		return m.cacheKWOKConfig()
	case v1alpha1.DistributionEKS:
		return m.cacheEKSConfig()
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

	// When using Hetzner provider, enable external cloud provider so that the
	// Cloud Controller Manager can initialize nodes with a providerID and write
	// node labels required by the CSI DaemonSet.
	if m.Config.Spec.Cluster.Provider == v1alpha1.ProviderHetzner {
		patches = append(patches, externalCloudProviderPatches()...)
	}

	return patches
}

func (m *ConfigManager) cacheVClusterConfig() error {
	configPath := m.Config.Spec.Cluster.DistributionConfig
	valuesPath := ""

	// Check if a vcluster.yaml values file exists at the configured path
	if configPath != "" {
		_, err := os.Stat(configPath)
		if err == nil {
			// File exists, use it as the values file
			valuesPath = configPath
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to stat vcluster config file: %w", err)
		}
		// If file doesn't exist, proceed with empty values (defaults only)
	}

	// Derive the cluster name from context or use default
	clusterName := m.resolveVClusterName()

	m.DistributionConfig.VCluster = &clusterprovisioner.VClusterConfig{
		Name:       clusterName,
		ValuesPath: valuesPath,
	}

	return nil
}

// resolveVClusterName extracts the cluster name from the configured context
// or falls back to the default vCluster cluster name.
func (m *ConfigManager) resolveVClusterName() string {
	ctx := m.Config.Spec.Cluster.Connection.Context
	if ctx != "" {
		// vCluster Docker context pattern: "vcluster-docker_<cluster-name>"
		const prefix = "vcluster-docker_"

		if name, ok := strings.CutPrefix(ctx, prefix); ok && name != "" {
			return name
		}
	}

	return "vcluster-default"
}

func (m *ConfigManager) cacheKWOKConfig() error {
	configPath := m.Config.Spec.Cluster.DistributionConfig
	resolvedPath := ""

	if configPath != "" {
		_, err := os.Stat(configPath)
		if err == nil {
			resolvedPath = configPath
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to stat KWOK config file: %w", err)
		}
	}

	clusterName := m.resolveKWOKName()

	m.DistributionConfig.KWOK = &clusterprovisioner.KWOKConfig{
		Name:       clusterName,
		ConfigPath: resolvedPath,
	}

	return nil
}

// resolveKWOKName extracts the cluster name from the configured context
// or falls back to the default KWOK cluster name.
func (m *ConfigManager) resolveKWOKName() string {
	ctx := strings.TrimSpace(m.Config.Spec.Cluster.Connection.Context)
	if ctx != "" {
		// KWOK context pattern: "kwok-<cluster-name>"
		const prefix = "kwok-"

		if name, ok := strings.CutPrefix(ctx, prefix); ok && name != "" {
			return strings.TrimSpace(name)
		}
	}

	return "kwok-default"
}

// cacheEKSConfig caches EKS configuration. The eks.yaml path comes from
// spec.cluster.distributionConfig (defaulting to "eks.yaml"). The cluster
// name and region are read from the eks.yaml metadata when present, with
// the kubeconfig context as fallback for the name.
func (m *ConfigManager) cacheEKSConfig() error {
	configPath := strings.TrimSpace(m.Config.Spec.Cluster.DistributionConfig)
	if configPath == "" {
		configPath = "eks.yaml"
	}

	resolvedPath, name, region, err := readEKSConfigMetadata(configPath)
	if err != nil {
		return err
	}

	if name == "" {
		name = m.resolveEKSNameFromContext()
	}

	m.DistributionConfig.EKS = &clusterprovisioner.EKSConfig{
		Name:       name,
		Region:     region,
		ConfigPath: resolvedPath,
	}

	return nil
}

// readEKSConfigMetadata reads the eksctl config file at configPath (if it
// exists) and returns its canonical path plus the parsed metadata.name /
// metadata.region fields. A missing file returns empty strings and no error
// so callers can fall back to context-based defaults.
func readEKSConfigMetadata(configPath string) (string, string, string, error) {
	_, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", "", nil
		}

		return "", "", "", fmt.Errorf("failed to stat EKS config file: %w", err)
	}

	canonical, err := fsutil.EvalCanonicalPath(configPath)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to canonicalize EKS config path: %w", err)
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(canonical), canonical)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read EKS config file: %w", err)
	}

	var meta struct {
		Metadata struct {
			Name   string `json:"name"`
			Region string `json:"region"`
		} `json:"metadata"`
	}

	err = yaml.Unmarshal(data, &meta)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse EKS config file: %w", err)
	}

	return canonical, meta.Metadata.Name, meta.Metadata.Region, nil
}

// resolveEKSNameFromContext extracts the cluster name from an EKS kubeconfig
// context of the form "<iam-identity>@<name>.<region>.eksctl.io", falling
// back to "eks-default".
func (m *ConfigManager) resolveEKSNameFromContext() string {
	ctx := strings.TrimSpace(m.Config.Spec.Cluster.Connection.Context)
	if ctx == "" {
		return "eks-default"
	}

	if idx := strings.LastIndex(ctx, "@"); idx >= 0 && idx+1 < len(ctx) {
		ctx = ctx[idx+1:]
	}

	if trimmed, ok := strings.CutSuffix(ctx, ".eksctl.io"); ok {
		if dot := strings.LastIndex(trimmed, "."); dot > 0 {
			trimmed = trimmed[:dot]
		}

		if trimmed != "" {
			return trimmed
		}
	}

	return "eks-default"
}

// externalCloudProviderPatches returns Talos patches that enable the external cloud provider.
// This configures both the cluster-level externalCloudProvider and the kubelet cloud-provider
// flag, which are required for cloud controller managers (e.g., Hetzner CCM) to:
//  1. Initialize nodes with a providerID (spec.providerID)
//  2. Write node labels that CSI DaemonSets require for scheduling
//
// See: https://www.talos.dev/latest/kubernetes-guides/configuration/cloud-provider/
func externalCloudProviderPatches() []talosconfigmanager.Patch {
	return []talosconfigmanager.Patch{
		{
			Path:    "external-cloud-provider",
			Scope:   talosconfigmanager.PatchScopeCluster,
			Content: []byte(talosgenerator.ExternalCloudProviderPatchYAML),
		},
	}
}

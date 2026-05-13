package configmanager

import (
	"errors"
	"fmt"
	"net"
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
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
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

	// Align the version contract with the pinned Talos version so that
	// generated machine configs only use fields the target version supports.
	contractErr := applyPinnedVersionContract(
		m.Config.Spec.Cluster.Talos.Version, talosManager,
	)
	if contractErr != nil {
		return nil, contractErr
	}

	// Add Hetzner-specific patches (external cloud provider + ingress firewall).
	err = m.addHetznerPatches(talosManager, patchesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to add Hetzner patches: %w", err)
	}

	// Inject kubelet cert rotation + CSR approver patches at runtime when
	// metrics-server is enabled AND both patch files don't already exist on disk.
	// Projects initialized with v7.4.0+ have the patch files; older projects
	// or manually-managed talos/ directories may not. The runtime injection
	// ensures the approver is always present without duplicating patches.
	// If stat fails for any reason other than the file not existing
	// (e.g., permissions), we inject the patches as a fail-safe.
	m.addKubeletCertRotationPatches(talosManager, patchesDir)

	// Inject worker role label patch at runtime when the patch file doesn't
	// already exist on disk. Projects initialized with this version+ have
	// the patch file; older projects or manually-managed talos/ directories
	// may not. The runtime injection ensures worker nodes always get the
	// node-role.kubernetes.io/worker label.
	m.addWorkerRoleLabelPatch(talosManager, patchesDir)

	// Wire extensions from ksail.yaml so that machine.install.image is patched
	// to use a Talos Image Factory installer containing the requested extensions.
	// Skip when an explicit SchematicID is set — it takes precedence over extensions.
	// Normalize first so whitespace-only or empty entries don't trigger schematic computation.
	if strings.TrimSpace(m.Config.Spec.Cluster.Talos.SchematicID) == "" {
		normalized := talosconfigmanager.NormalizeExtensions(m.Config.Spec.Cluster.Talos.Extensions)
		if len(normalized) > 0 {
			talosManager.WithExtensions(normalized)
		}
	}

	config, err := talosManager.Load(configmanagerinterface.LoadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to load Talos config: %w", err)
	}

	return config, nil
}

// addHetznerPatches injects Hetzner-specific runtime patches into talosManager:
//   - external cloud provider patch (always, for CCM node initialization)
//   - ingress firewall patches (when IngressFirewall is not Disabled AND patch files
//     are not already present on disk, to avoid duplicates with init-generated files)
func (m *ConfigManager) addHetznerPatches(
	talosManager *talosconfigmanager.ConfigManager,
	patchesDir string,
) error {
	if m.Config.Spec.Cluster.Provider != v1alpha1.ProviderHetzner {
		return nil
	}

	talosManager.WithAdditionalPatches(externalCloudProviderPatches())

	if m.Config.Spec.Provider.Hetzner.IngressFirewall != v1alpha1.IngressFirewallDisabled &&
		!ingressFirewallPatchFilesExist(patchesDir) {
		patches, err := ingressFirewallPatches(
			v1alpha1.HetznerNetworkCIDR(m.Config.Spec),
			v1alpha1.HetznerCNIPort(m.Config.Spec),
			m.Config.Spec.Provider.Hetzner.AllowedCIDRs,
		)
		if err != nil {
			return err
		}

		talosManager.WithAdditionalPatches(patches)
	}

	return nil
}

// addKubeletCertRotationPatches injects kubelet cert-rotation and CSR-approver patches
// at runtime when MetricsServer is enabled and the patch files do not exist on disk.
func (m *ConfigManager) addKubeletCertRotationPatches(
	talosManager *talosconfigmanager.ConfigManager,
	patchesDir string,
) {
	if m.Config.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled &&
		!kubeletPatchFilesExist(patchesDir) {
		talosManager.WithAdditionalPatches(kubeletCertRotationAndApproverPatches())
	}
}

// legacyWorkerRoleLabelPatchYAML is the exact content of the old scaffold that
// used machine.nodeLabels. Only files matching this content exactly are migrated.
const legacyWorkerRoleLabelPatchYAML = "machine:\n  nodeLabels:\n    node-role.kubernetes.io/worker: \"\"\n"

// addWorkerRoleLabelPatch injects the worker role label patch at runtime
// when the patch file does not exist on disk. This ensures existing
// scaffolded projects (created before this patch was introduced) still
// get the node-role.kubernetes.io/worker label on worker nodes.
//
// It also migrates existing patch files that use the legacy machine.nodeLabels
// format to the kubelet.extraArgs["node-labels"] format. The old format causes
// the Kubernetes NodeRestriction admission controller to block ALL label updates
// from Talos's NodeApplyController on worker nodes.
func (m *ConfigManager) addWorkerRoleLabelPatch(
	talosManager *talosconfigmanager.ConfigManager,
	patchesDir string,
) {
	if !workerRoleLabelPatchFileExists(patchesDir) {
		talosManager.WithAdditionalPatches([]talosconfigmanager.Patch{workerRoleLabelPatch()})

		return
	}

	// Migrate stale patch files that use machine.nodeLabels (broken on Hetzner
	// due to NodeRestriction blocking kubelet from setting node-role.kubernetes.io/*).
	patchFile := filepath.Join(patchesDir, "workers", "worker-role-label.yaml")

	content, err := fsutil.ReadFileSafe(patchesDir, patchFile)
	if err != nil {
		return
	}

	// Only migrate files that exactly match the legacy scaffold. User-customized
	// files (with additional labels or settings) are left untouched.
	if strings.TrimSpace(string(content)) == strings.TrimSpace(legacyWorkerRoleLabelPatchYAML) {
		canonPath, pathErr := fsutil.EvalCanonicalPath(patchFile)
		if pathErr != nil {
			return
		}

		//nolint:mnd // standard restrictive file permission
		if writeErr := os.WriteFile(canonPath, []byte(talosgenerator.WorkerRoleLabelPatchYAML), 0o600); writeErr != nil {
			// Write failed — inject the correct runtime patch as fallback so the
			// worker role label is still set via kubelet.extraArgs at registration time.
			talosManager.WithAdditionalPatches([]talosconfigmanager.Patch{workerRoleLabelPatch()})
		}
	}
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

	// When using a non-default CNI (e.g., Cilium, Calico), disable Talos's built-in
	// Flannel CNI. Without this patch, Talos installs Flannel which conflicts with the
	// custom CNI and prevents pods (including kubelet-serving-cert-approver) from
	// networking correctly.
	if m.Config.Spec.Cluster.CNI != v1alpha1.CNIDefault && m.Config.Spec.Cluster.CNI != "" {
		patches = append(patches, disableDefaultCNIPatch())
	}

	// When metrics-server is enabled on Talos, we need two patches:
	// 1. Enable kubelet certificate rotation (rotate-server-certificates: true)
	// 2. Install kubelet-serving-cert-approver via inlineManifests to approve the CSRs
	//
	// Note: We use alex1989hu/kubelet-serving-cert-approver for Talos because it provides
	// a lightweight standalone manifest. For non-Talos distributions,
	// postfinance/kubelet-csr-approver is used via Helm post-bootstrap.
	//
	// The manifest is embedded in the binary via inlineManifests (not fetched from a URL)
	// to eliminate external network dependencies during Talos bootstrap.
	//
	// See: https://docs.siderolabs.com/kubernetes-guides/monitoring-and-observability/deploy-metrics-server/
	if m.Config.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerEnabled {
		patches = append(patches, kubeletCertRotationAndApproverPatches()...)
	}

	// When using Hetzner provider, enable external cloud provider so that the
	// Cloud Controller Manager can initialize nodes with a providerID and write
	// node labels required by the CSI DaemonSet.
	if m.Config.Spec.Cluster.Provider == v1alpha1.ProviderHetzner {
		patches = append(patches, externalCloudProviderPatches()...)
	}

	// Always label worker nodes with node-role.kubernetes.io/worker.
	// This is worker-scoped so it only affects worker configs.
	patches = append(patches, workerRoleLabelPatch())

	return patches
}

// disableDefaultCNIPatch returns a Talos machine config patch that disables the default
// CNI (Flannel). Used for runtime injection when no scaffolded project exists (init=false)
// and a non-default CNI (Cilium, Calico) is requested.
func disableDefaultCNIPatch() talosconfigmanager.Patch {
	return talosconfigmanager.Patch{
		Path:    "disable-default-cni",
		Scope:   talosconfigmanager.PatchScopeCluster,
		Content: []byte(talosgenerator.DisableDefaultCNIPatchYAML),
	}
}

// kubeletCertRotationAndApproverPatches returns the patches for enabling kubelet
// certificate rotation and installing the CSR approver via inlineManifests.
func kubeletCertRotationAndApproverPatches() []talosconfigmanager.Patch {
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

	// Patch 2: Install kubelet-serving-cert-approver via inlineManifests
	kubeletCSRApproverPatch := talosconfigmanager.Patch{
		Path:    "kubelet-csr-approver-inlinemanifest",
		Scope:   talosconfigmanager.PatchScopeCluster,
		Content: []byte(talosgenerator.KubeletCSRApproverInlineManifestPatchYAML()),
	}

	return []talosconfigmanager.Patch{kubeletCertRotationPatch, kubeletCSRApproverPatch}
}

// kubeletPatchFilesExist returns true when BOTH kubelet cert rotation and
// CSR approver patch files exist on disk. If either file is missing or
// stat fails for any reason (permissions, broken symlinks), returns false
// so that runtime patches are injected as a fail-safe.
func kubeletPatchFilesExist(patchesDir string) bool {
	certRotation := filepath.Join(patchesDir, "cluster", "kubelet-cert-rotation.yaml")
	csrApprover := filepath.Join(patchesDir, "cluster", "kubelet-csr-approver.yaml")

	_, err1 := os.Stat(certRotation)
	_, err2 := os.Stat(csrApprover)

	return err1 == nil && err2 == nil
}

// workerRoleLabelPatchFileExists returns true when the worker role label
// patch file exists on disk. If the file is missing or stat fails for any
// reason, returns false so that the runtime patch is injected as a fail-safe.
func workerRoleLabelPatchFileExists(patchesDir string) bool {
	patchFile := filepath.Join(patchesDir, "workers", "worker-role-label.yaml")
	_, err := os.Stat(patchFile)

	return err == nil
}

// ingressFirewallPatchPaths returns the expected ingress firewall patch file paths
// generated by ksail cluster init.
func ingressFirewallPatchPaths(patchesDir string) (string, string, string) {
	defaultAction := filepath.Join(patchesDir, "cluster", "ingress-firewall-default-action.yaml")
	cpRules := filepath.Join(patchesDir, "control-planes", "ingress-firewall-rules.yaml")
	workerRules := filepath.Join(patchesDir, "workers", "ingress-firewall-rules.yaml")

	return defaultAction, cpRules, workerRules
}

// ingressFirewallPatchFilesExist returns true when ALL three ingress firewall patch
// files exist on disk (generated by ksail cluster init). Returns false if any file is
// missing or stat fails, so runtime patches are injected as a fail-safe.
func ingressFirewallPatchFilesExist(patchesDir string) bool {
	defaultAction, cpRules, workerRules := ingressFirewallPatchPaths(patchesDir)

	_, err1 := os.Stat(defaultAction)
	_, err2 := os.Stat(cpRules)
	_, err3 := os.Stat(workerRules)

	return err1 == nil && err2 == nil && err3 == nil
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

// workerRoleLabelPatch returns a Talos machine config patch that labels worker nodes
// with node-role.kubernetes.io/worker. Used for runtime injection when no scaffolded
// project exists (init=false). Worker-scoped so it only affects worker configs.
func workerRoleLabelPatch() talosconfigmanager.Patch {
	return talosconfigmanager.Patch{
		Path:    "worker-role-label",
		Scope:   talosconfigmanager.PatchScopeWorker,
		Content: []byte(talosgenerator.WorkerRoleLabelPatchYAML),
	}
}

var (
	errIngressFirewallMissingCIDR = errors.New(
		"networkCIDR is required for ingress firewall patches",
	)
	errIngressFirewallInvalidCIDR = errors.New("networkCIDR is not a valid CIDR")
	errIngressFirewallInvalidPort = errors.New("cniPort must be between 1 and 65535")
)

// ingressFirewallPatches returns Talos patches that configure the OS-level ingress firewall.
// This generates three patches: a default action (ingress: block) for all nodes, plus
// per-role NetworkRuleConfig documents for control-plane and worker nodes.
// When allowedCIDRs is non-empty, the CP rules restrict apid and kubernetes-api access
// to the specified CIDR blocks instead of 0.0.0.0/0.
// See: https://www.talos.dev/latest/talos-guides/network/ingress-firewall/
func ingressFirewallPatches(
	networkCIDR string,
	cniPort int,
	allowedCIDRs []string,
) ([]talosconfigmanager.Patch, error) {
	cidr := strings.TrimSpace(networkCIDR)
	if cidr == "" {
		return nil, errIngressFirewallMissingCIDR
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errIngressFirewallInvalidCIDR, err)
	}

	if cniPort < 1 || cniPort > 65535 {
		return nil, fmt.Errorf("%w: got %d", errIngressFirewallInvalidPort, cniPort)
	}

	normalizedCIDR := ipNet.String()

	return []talosconfigmanager.Patch{
		{
			Path:    "ingress-firewall-default-action",
			Scope:   talosconfigmanager.PatchScopeCluster,
			Content: []byte(talosgenerator.IngressFirewallDefaultActionYAML),
		},
		{
			Path:  "ingress-firewall-cp-rules",
			Scope: talosconfigmanager.PatchScopeControlPlane,
			Content: []byte(
				talosgenerator.IngressFirewallCPRulesYAML(normalizedCIDR, cniPort, allowedCIDRs),
			),
		},
		{
			Path:    "ingress-firewall-worker-rules",
			Scope:   talosconfigmanager.PatchScopeWorker,
			Content: []byte(talosgenerator.IngressFirewallWorkerRulesYAML(normalizedCIDR, cniPort)),
		},
	}, nil
}

// applyPinnedVersionContract sets the version contract on the Talos config manager
// when a pinned Talos version is specified. Returns an error if the version cannot
// be parsed. Does nothing when pinnedVersion is empty.
func applyPinnedVersionContract(
	pinnedVersion string,
	talosManager *talosconfigmanager.ConfigManager,
) error {
	pinnedVersion = strings.TrimSpace(pinnedVersion)
	if pinnedVersion == "" {
		return nil
	}

	if !strings.HasPrefix(pinnedVersion, "v") {
		pinnedVersion = "v" + pinnedVersion
	}

	contract, err := talosconfig.ParseContractFromVersion(pinnedVersion)
	if err != nil {
		return fmt.Errorf(
			"parse Talos version contract for pinned version %q: %w",
			pinnedVersion,
			err,
		)
	}

	talosManager.WithVersionContract(contract)

	return nil
}

package configmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/container/apiv1/containerpb"
	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	gkeclient "github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"google.golang.org/protobuf/encoding/protojson"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

var errInvalidEKSConfig = errors.New("invalid EKS config file")

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
		m.resolveTalosKubernetesVersion(),
		"", // Use default network CIDR
	)
	talosManager.WithEnvLookup(m.Config.Spec.Cluster.LocalRegistry.PullEnvLookup(nil))

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

	// Enable the MutatingAdmissionPolicy feature gate for Calico (see method doc).
	m.addCalicoFeatureGatePatch(talosManager)

	// Inject kubelet cert rotation + CSR approver patches at runtime when
	// metrics-server is enabled AND both patch files don't already exist on disk.
	// Projects initialized with v7.4.0+ have the patch files; older projects
	// or manually-managed talos/ directories may not. The runtime injection
	// ensures the approver is always present without duplicating patches.
	// If stat fails for any reason other than the file not existing
	// (e.g., permissions), we inject the patches as a fail-safe.
	m.addKubeletCertRotationPatches(talosManager, patchesDir)

	// Migrate away the legacy worker role label patch. Older projects scaffolded a
	// talos/workers/worker-role-label.yaml that set node-role.kubernetes.io/worker via
	// kubelet --node-labels (or machine.nodeLabels). Kubernetes 1.33+ rejects
	// node-role.kubernetes.io/* labels passed through kubelet --node-labels, which crashes
	// every worker kubelet. The label is purely cosmetic and unused by ksail, so we drop it
	// and remove the stale patch file so it is no longer applied at create time.
	m.removeWorkerRoleLabelPatch(patchesDir)

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

// addCalicoFeatureGatePatch enables the MutatingAdmissionPolicy feature gate /
// v1beta1 admissionregistration API only for Calico, whose v3.30+ CRD chart ships
// MutatingAdmissionPolicy resources. Enabling it elsewhere makes other components
// (e.g. Kyverno) attempt to use the API and fail.
func (m *ConfigManager) addCalicoFeatureGatePatch(
	talosManager *talosconfigmanager.ConfigManager,
) {
	if m.Config.Spec.Cluster.CNI != v1alpha1.CNICalico {
		return
	}

	talosManager.WithAdditionalPatches(
		[]talosconfigmanager.Patch{talosconfigmanager.APIServerFeatureGatesPatch()},
	)
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

// legacyWorkerRoleLabelPatchYAML is the machine.nodeLabels form of the worker role label
// patch that older ksail versions scaffolded.
const legacyWorkerRoleLabelPatchYAML = `machine:
  nodeLabels:
    node-role.kubernetes.io/worker: ""
`

// kubeletWorkerRoleLabelPatchYAML is the kubelet --node-labels form of the worker role
// label patch that more recent ksail versions scaffolded.
const kubeletWorkerRoleLabelPatchYAML = `machine:
  kubelet:
    extraArgs:
      node-labels: "node-role.kubernetes.io/worker="
`

// workerRoleLabelFailureModes explains why a node-role.kubernetes.io/worker patch breaks
// worker nodes, covering both forms ksail historically generated. Shared across the migration
// warnings so the remediation guidance stays accurate regardless of which form is found.
const workerRoleLabelFailureModes = "Set via kubelet --node-labels the label is rejected by " +
	"Kubernetes 1.33+ (worker kubelets fail to start); set via machine.nodeLabels it is " +
	"blocked by the NodeRestriction admission controller after registration."

// removeWorkerRoleLabelPatch deletes a stale worker role label patch file from disk.
//
// ksail used to label worker nodes with node-role.kubernetes.io/worker, first via
// machine.nodeLabels and later via kubelet --node-labels. Kubernetes 1.33+ rejects
// node-role.kubernetes.io/* labels passed through kubelet --node-labels, which prevents
// every worker kubelet from starting and makes the cluster unusable. The label is purely
// cosmetic (it only populates the kubectl ROLE column) and nothing in ksail depends on it,
// so it is no longer set at all.
//
// The patch file is auto-discovered and applied at create time, so leaving it on disk would
// keep breaking existing projects. We delete it only when its content exactly matches one of
// the two known ksail-generated scaffolds. A user-customized file that still carries the label
// is left untouched, with a warning, so hand-edited config is never destroyed.
func (m *ConfigManager) removeWorkerRoleLabelPatch(patchesDir string) {
	patchFile := filepath.Join(patchesDir, "workers", "worker-role-label.yaml")

	content, err := fsutil.ReadFileSafe(patchesDir, patchFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No patch file — the common case, nothing to migrate.
			return
		}

		// The file exists but could not be read safely (e.g. a symlink escaping
		// patchesDir, or a permission error). Warn rather than silently leaving a
		// patch that may break worker kubelets.
		m.warnWorkerRoleLabel(
			"could not read talos/workers/worker-role-label.yaml (" + err.Error() +
				"); if it sets node-role.kubernetes.io/worker, remove it manually. " +
				workerRoleLabelFailureModes,
		)

		return
	}

	contentStr := strings.TrimSpace(string(content))

	isKnownScaffold := contentStr == strings.TrimSpace(legacyWorkerRoleLabelPatchYAML) ||
		contentStr == strings.TrimSpace(kubeletWorkerRoleLabelPatchYAML)

	if isKnownScaffold {
		// Remove the entry at its declared path. os.Remove does not follow the final
		// symlink, so a symlinked entry is unlinked instead of deleting its target.
		removeErr := os.Remove(patchFile)
		if removeErr != nil {
			m.warnWorkerRoleLabel(
				"could not delete the obsolete talos/workers/worker-role-label.yaml (" +
					removeErr.Error() + "); it sets node-role.kubernetes.io/worker, so " +
					"remove it manually. " + workerRoleLabelFailureModes,
			)
		}

		return
	}

	if strings.Contains(contentStr, "node-role.kubernetes.io/worker") {
		m.warnWorkerRoleLabel(
			"talos/workers/worker-role-label.yaml sets node-role.kubernetes.io/worker; " +
				"remove that label. " + workerRoleLabelFailureModes,
		)
	}
}

// resolveTalosKubernetesVersion resolves the Kubernetes version for Talos config
// generation: it honours an explicit pin (spec.cluster.kubernetesVersion), otherwise
// uses the built-in default capped to one the pinned Talos release
// (spec.cluster.talos.version) supports, warning when the default is capped. On an
// existing cluster the provisioner further prefers the running version when unpinned,
// so an unrelated update never forces a Kubernetes upgrade.
func (m *ConfigManager) resolveTalosKubernetesVersion() string {
	version := talosconfigmanager.ResolveKubernetesVersion(
		m.Config.Spec.Cluster.Talos.Version,
		m.Config.Spec.Cluster.KubernetesVersion,
	)
	warnKubernetesVersionCapped(m.Config, version, m.Writer)

	return version
}

// warnKubernetesVersionCapped emits a warning when KSail had to cap its built-in
// default Kubernetes version to keep it compatible with the pinned Talos release.
// It is a no-op when the user pinned a Kubernetes version explicitly, when no
// Talos version is pinned, or when the default needed no capping — so it only
// surfaces in the narrow case it is meant to explain.
func warnKubernetesVersionCapped(cfg *v1alpha1.Cluster, resolvedVersion string, out io.Writer) {
	if strings.TrimSpace(cfg.Spec.Cluster.KubernetesVersion) != "" {
		return
	}

	if strings.TrimSpace(cfg.Spec.Cluster.Talos.Version) == "" {
		return
	}

	if resolvedVersion == talosconfigmanager.DefaultKubernetesVersion {
		return
	}

	notify.WriteMessage(notify.Message{
		Type: notify.WarningType,
		Content: fmt.Sprintf(
			"Kubernetes %s is too new for the pinned Talos version %s; "+
				"defaulting to compatible Kubernetes %s. "+
				"Set spec.cluster.kubernetesVersion to choose a different version.",
			talosconfigmanager.DefaultKubernetesVersion,
			cfg.Spec.Cluster.Talos.Version,
			resolvedVersion,
		),
		Writer: out,
	})
}

// warnWorkerRoleLabel emits a warning about a stale or customized worker role label patch
// that still sets node-role.kubernetes.io/worker.
func (m *ConfigManager) warnWorkerRoleLabel(content string) {
	notify.WriteMessage(notify.Message{
		Type:    notify.WarningType,
		Content: content,
		Writer:  m.Writer,
	})
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
	case v1alpha1.DistributionGKE:
		return m.cacheGKEConfig()
	case v1alpha1.DistributionAKS:
		return m.cacheAKSConfig()
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

		// Resolve the Kubernetes version here too (not just in loadTalosConfig): with
		// no scaffolded talos/ dir, this fallback must still honor a pin or cap the
		// default to the pinned Talos version — otherwise a pinned older Talos would
		// be paired with an incompatible default Kubernetes version.
		versionContract, contractErr := talosconfigmanager.ParseVersionContract(
			m.Config.Spec.Cluster.Talos.Version,
		)
		if contractErr != nil {
			return fmt.Errorf("resolve pinned Talos version contract: %w", contractErr)
		}

		talosConfig, err = talosconfigmanager.NewDefaultConfigsWithVersionContractAndPatches(
			m.resolveTalosKubernetesVersion(), versionContract, patches,
		)
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

	return patches
}

// disableDefaultCNIPatch returns a Talos machine config patch that disables the default
// CNI (Flannel). Used for runtime injection when no scaffolded project exists (init=false)
// and a non-default CNI (Cilium, Calico) is requested.
func disableDefaultCNIPatch() talosconfigmanager.Patch {
	return talosconfigmanager.Patch{
		Path:    "disable-default-cni",
		Scope:   talosconfigmanager.PatchScopeCluster,
		Content: []byte(talosconfigmanager.DisableDefaultCNIPatchYAML(false)),
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

// ingressFirewallPatchPaths returns the expected ingress firewall patch file paths
// generated by ksail project init.
func ingressFirewallPatchPaths(patchesDir string) (string, string, string) {
	defaultAction := filepath.Join(patchesDir, "cluster", "ingress-firewall-default-action.yaml")
	cpRules := filepath.Join(patchesDir, "control-planes", "ingress-firewall-rules.yaml")
	workerRules := filepath.Join(patchesDir, "workers", "ingress-firewall-rules.yaml")

	return defaultAction, cpRules, workerRules
}

// ingressFirewallPatchFilesExist returns true when ALL three ingress firewall patch
// files exist on disk (generated by ksail project init). Returns false if any file is
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

	nameFromConfig := strings.TrimSpace(name) != ""

	if name == "" {
		name = m.resolveEKSNameFromContext()
	}

	m.DistributionConfig.EKS = &clusterprovisioner.EKSConfig{
		Name:           name,
		NameFromConfig: nameFromConfig,
		Region:         region,
		ConfigPath:     resolvedPath,
	}

	return nil
}

// readEKSConfigMetadata reads the eksctl config file at configPath (if it
// exists) and returns its canonical path plus the parsed metadata.name /
// metadata.region fields. A missing file returns empty strings and no error
// so callers can fall back to context-based defaults. A present file must be
// the supported eksctl ClusterConfig shape before its metadata can establish
// lifecycle ownership provenance.
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
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name   string `json:"name"`
			Region string `json:"region"`
		} `json:"metadata"`
	}

	err = yaml.Unmarshal(data, &meta)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse EKS config file: %w", err)
	}

	const (
		eksConfigAPIVersion = "eksctl.io/v1alpha5"
		eksConfigKind       = "ClusterConfig"
	)

	if meta.APIVersion != eksConfigAPIVersion || meta.Kind != eksConfigKind {
		return "", "", "", fmt.Errorf(
			"%w: expected apiVersion %q and kind %q, got %q and %q",
			errInvalidEKSConfig,
			eksConfigAPIVersion,
			eksConfigKind,
			meta.APIVersion,
			meta.Kind,
		)
	}

	name := meta.Metadata.Name
	if name != "" {
		if name != strings.TrimSpace(name) {
			return "", "", "", fmt.Errorf(
				"%w: metadata.name %q has leading or trailing whitespace",
				errInvalidEKSConfig,
				name,
			)
		}

		err = v1alpha1.ValidateClusterName(name)
		if err != nil {
			return "", "", "", fmt.Errorf(
				"%w: metadata.name: %w",
				errInvalidEKSConfig,
				err,
			)
		}
	}

	return canonical, name, meta.Metadata.Region, nil
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

// Default environment variables for the GKE project/location resolution
// (mirror OptionsGCP's struct-tag defaults).
const (
	defaultGCPProjectEnvVar  = "GOOGLE_CLOUD_PROJECT"
	defaultGCPLocationEnvVar = "GOOGLE_CLOUD_LOCATION"
)

// cacheGKEConfig caches GKE configuration. The gke.yaml path comes from
// spec.cluster.distributionConfig (defaulting to "gke.yaml") and, when the
// file exists, is parsed as a declarative containerpb.Cluster spec (the shape
// the GKE API's create call consumes). The cluster name is read from the spec
// when present, with the kubeconfig context as fallback; the project — which
// is API-call scope, not part of the cluster spec — and a location override
// resolve from the environment variables named by spec.provider.gcp.
func (m *ConfigManager) cacheGKEConfig() error {
	configPath := strings.TrimSpace(m.Config.Spec.Cluster.DistributionConfig)
	if configPath == "" {
		configPath = v1alpha1.DefaultGKEDistributionConfig
	}

	resolvedPath, clusterSpec, err := readGKEConfigSpec(configPath)
	if err != nil {
		return err
	}

	name := clusterSpec.GetName()
	if name == "" {
		name = m.resolveGKENameFromContext()
	}

	location := envValue(
		m.Config.Spec.Provider.GCP.LocationEnvVar, defaultGCPLocationEnvVar,
	)
	if location == "" {
		location = clusterSpec.GetLocation()
	}

	m.DistributionConfig.GKE = &clusterprovisioner.GKEConfig{
		Name: name,
		Project: envValue(
			m.Config.Spec.Provider.GCP.ProjectEnvVar, defaultGCPProjectEnvVar,
		),
		Location:    location,
		ConfigPath:  resolvedPath,
		ClusterSpec: clusterSpec,
	}

	return nil
}

// envValue reads the environment variable named by envVar, falling back to
// fallbackVar when the cluster spec does not name one.
func envValue(envVar, fallbackVar string) string {
	if envVar == "" {
		envVar = fallbackVar
	}

	return os.Getenv(envVar)
}

// readCloudConfigJSON stats and reads the cloud distribution config at
// configPath, converting its YAML to JSON for the caller's spec unmarshaller.
// A missing file returns ("", nil, nil) so callers fall back to context-based
// defaults; kind names the distribution in error messages.
func readCloudConfigJSON(configPath, kind string) (string, []byte, error) {
	_, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, nil
		}

		return "", nil, fmt.Errorf("failed to stat %s config file: %w", kind, err)
	}

	canonical, err := fsutil.EvalCanonicalPath(configPath)
	if err != nil {
		return "", nil, fmt.Errorf("failed to canonicalize %s config path: %w", kind, err)
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(canonical), canonical)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read %s config file: %w", kind, err)
	}

	jsonData, err := yaml.YAMLToJSON(data)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse %s config file: %w", kind, err)
	}

	return canonical, jsonData, nil
}

// readGKEConfigSpec reads the gke.yaml at configPath (if it exists) and parses
// it as a containerpb.Cluster via the proto JSON mapping (YAML keys use the
// proto3 JSON camelCase names). A missing file returns a nil spec and no error
// so callers fall back to context-based defaults; inspection-only operations
// work without a spec, while create requires one and fails clearly without it.
func readGKEConfigSpec(configPath string) (string, *containerpb.Cluster, error) {
	canonical, jsonData, err := readCloudConfigJSON(configPath, "GKE")
	if err != nil || jsonData == nil {
		return canonical, nil, err
	}

	clusterSpec := &containerpb.Cluster{}

	err = protojson.Unmarshal(jsonData, clusterSpec)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse GKE cluster spec: %w", err)
	}

	return canonical, clusterSpec, nil
}

// resolveGKENameFromContext extracts the cluster name from a GKE kubeconfig
// context (gcloud convention: gke_<project>_<location>_<name>), falling back
// to the distribution default when the context is absent or not GKE-shaped.
// Parsing delegates to the gke client package's single context parser.
func (m *ConfigManager) resolveGKENameFromContext() string {
	name := gkeclient.ClusterNameFromContext(m.Config.Spec.Cluster.Connection.Context)
	if name != "" {
		return name
	}

	return "gke-default"
}

// Default environment variables for the Azure subscription/resource-group
// resolution (mirror OptionsAzure's struct-tag defaults).
const (
	defaultAzureSubscriptionIDEnvVar = "AZURE_SUBSCRIPTION_ID"
	defaultAzureResourceGroupEnvVar  = "AZURE_RESOURCE_GROUP"
)

// cacheAKSConfig caches AKS configuration. The aks.yaml path comes from
// spec.cluster.distributionConfig (defaulting to "aks.yaml") and, when the
// file exists, is parsed as a declarative armcontainerservice.ManagedCluster
// spec (the shape the AKS API's create call consumes). The cluster name is
// read from the spec when present, with the kubeconfig context as fallback;
// the subscription and resource group — which are API-call scope, not part of
// the cluster spec — resolve from the environment variables named by
// spec.provider.azure.
func (m *ConfigManager) cacheAKSConfig() error {
	configPath := strings.TrimSpace(m.Config.Spec.Cluster.DistributionConfig)
	if configPath == "" {
		configPath = v1alpha1.DefaultAKSDistributionConfig
	}

	resolvedPath, clusterSpec, err := readAKSConfigSpec(configPath)
	if err != nil {
		return err
	}

	name := ""
	if clusterSpec != nil && clusterSpec.Name != nil {
		name = *clusterSpec.Name
	}

	if name == "" {
		name = m.resolveAKSNameFromContext()
	}

	m.DistributionConfig.AKS = &clusterprovisioner.AKSConfig{
		Name: name,
		SubscriptionID: envValue(
			m.Config.Spec.Provider.Azure.SubscriptionIDEnvVar, defaultAzureSubscriptionIDEnvVar,
		),
		ResourceGroup: envValue(
			m.Config.Spec.Provider.Azure.ResourceGroupEnvVar, defaultAzureResourceGroupEnvVar,
		),
		ConfigPath:  resolvedPath,
		ClusterSpec: clusterSpec,
	}

	return nil
}

// readAKSConfigSpec reads the aks.yaml at configPath (if it exists) and parses
// it as an armcontainerservice.ManagedCluster via the ARM SDK's JSON mapping
// (YAML keys use the ARM API's camelCase names). A missing file returns a nil
// spec and no error so callers fall back to context-based defaults;
// inspection-only operations work without a spec, while create requires one
// and fails clearly without it.
func readAKSConfigSpec(configPath string) (string, *armcontainerservice.ManagedCluster, error) {
	canonical, jsonData, err := readCloudConfigJSON(configPath, "AKS")
	if err != nil || jsonData == nil {
		return canonical, nil, err
	}

	clusterSpec := &armcontainerservice.ManagedCluster{}

	err = json.Unmarshal(jsonData, clusterSpec)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse AKS cluster spec: %w", err)
	}

	return canonical, clusterSpec, nil
}

// resolveAKSNameFromContext falls back to the kubeconfig context for the
// cluster name — az aks get-credentials names the context after the cluster
// itself — and to the distribution default when no context is configured.
func (m *ConfigManager) resolveAKSNameFromContext() string {
	name := strings.TrimSpace(m.Config.Spec.Cluster.Connection.Context)
	if name != "" {
		return name
	}

	return "aks-default"
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
// be parsed. An empty pin retains the conservative Talos 1.12 default contract.
func applyPinnedVersionContract(
	pinnedVersion string,
	talosManager *talosconfigmanager.ConfigManager,
) error {
	contract, err := talosconfigmanager.ParseVersionContract(pinnedVersion)
	if err != nil {
		return fmt.Errorf("resolve pinned Talos version contract: %w", err)
	}

	if contract != nil {
		talosManager.WithVersionContract(contract)
	}

	return nil
}

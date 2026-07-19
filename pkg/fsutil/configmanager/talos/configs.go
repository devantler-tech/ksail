package talos

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"sigs.k8s.io/yaml"
)

// Configs holds the loaded Talos machine configurations with patches applied.
// It wraps the upstream bundle.Bundle and provides convenient accessors.
//
// Usage:
//
//	manager := NewConfigManager("talos", "my-cluster", "1.32.0", "10.5.0.0/24")
//	configs, err := manager.Load(configmanager.LoadOptions{})
//	if err != nil {
//	    return err
//	}
//
//	// Access programmatically
//	cpConfig := configs.ControlPlane()
//	workerConfig := configs.Worker()
//
//	// Access specific config sections (Talos alpha.2 multi-document accessors)
//	flannelEnabled := cpConfig.K8sFlannelCNIConfig() != nil
//	kubeletImage := cpConfig.Machine().Kubelet().Image()
type Configs struct {
	// Name is the cluster name.
	Name string
	// bundle is the underlying Talos config bundle.
	bundle *bundle.Bundle
	// kubernetesVersion is stored for regeneration with a new name.
	kubernetesVersion string
	// networkCIDR is stored for regeneration with a new name.
	networkCIDR string
	// endpoint is an optional override endpoint IP for cloud deployments (e.g., Hetzner public IP).
	// When empty, the endpoint is calculated from networkCIDR.
	endpoint string
	// patches is stored for regeneration with a new name.
	patches []Patch
	// versionContract is stored for regeneration to preserve version contract across WithName/WithEndpoint calls.
	versionContract *talosconfig.VersionContract
	// extensions is the list of Talos Image Factory official extension names.
	// When non-empty, machine.install.image is patched to use a factory installer
	// image and a schematic ID is computed.
	extensions []string
	// schematicID is the computed schematic ID from extensions.
	schematicID string
}

// NewDefaultConfigs creates a new Talos Configs with default settings.
// This is used when no scaffolded project exists and default configurations are needed.
// It creates a valid config bundle with:
//   - Cluster name: DefaultClusterName ("talos-default")
//   - Kubernetes version: DefaultKubernetesVersion ("1.36.0")
//   - Network CIDR: DefaultNetworkCIDR ("10.5.0.0/24")
//   - allowSchedulingOnControlPlanes: true (for single-node/control-plane-only clusters)
func NewDefaultConfigs() (*Configs, error) {
	// Default configs are used for control-plane-only clusters (no workers),
	// so we need to allow scheduling on control-plane nodes.
	allowSchedulingPatch := Patch{
		Path:    "allow-scheduling-on-control-planes",
		Scope:   PatchScopeCluster,
		Content: []byte("cluster:\n  allowSchedulingOnControlPlanes: true\n"),
	}

	return newConfigs(
		DefaultClusterName,
		DefaultKubernetesVersion,
		DefaultNetworkCIDR,
		[]Patch{allowSchedulingPatch},
		nil,
	)
}

// NewDefaultConfigsWithPatches creates a new Talos Configs with default settings plus additional patches.
// This is used when no scaffolded project exists but additional runtime patches are needed
// (e.g., kubelet-csr-approver inlineManifests when metrics-server is enabled).
//
// The additional patches are applied after the default allowSchedulingOnControlPlanes patch.
// The Kubernetes version is DefaultKubernetesVersion; use
// NewDefaultConfigsWithVersionAndPatches to honor a pin or a Talos-compatible default.
func NewDefaultConfigsWithPatches(additionalPatches []Patch) (*Configs, error) {
	return NewDefaultConfigsWithVersionAndPatches(DefaultKubernetesVersion, additionalPatches)
}

// NewDefaultConfigsWithVersionAndPatches is like NewDefaultConfigsWithPatches but
// targets a specific Kubernetes version. It is used when no scaffolded talos/ dir
// exists so the default config still honors an explicit pin or a Talos-compatible
// default (spec.cluster.kubernetesVersion / capped default) rather than always
// using DefaultKubernetesVersion. An empty kubernetesVersion falls back to the default.
func NewDefaultConfigsWithVersionAndPatches(
	kubernetesVersion string,
	additionalPatches []Patch,
) (*Configs, error) {
	return NewDefaultConfigsWithVersionContractAndPatches(
		kubernetesVersion,
		nil,
		additionalPatches,
	)
}

// NewDefaultConfigsWithVersionContractAndPatches builds the no-scaffold
// default bundle for a specific Talos version contract. Version-gated patches
// are migrated before bundle generation so their shape matches the generated
// configuration documents.
func NewDefaultConfigsWithVersionContractAndPatches(
	kubernetesVersion string,
	versionContract *talosconfig.VersionContract,
	additionalPatches []Patch,
) (*Configs, error) {
	if kubernetesVersion == "" {
		kubernetesVersion = DefaultKubernetesVersion
	}

	// Default configs are used for control-plane-only clusters (no workers),
	// so we need to allow scheduling on control-plane nodes.
	allowSchedulingPatch := Patch{
		Path:    "allow-scheduling-on-control-planes",
		Scope:   PatchScopeCluster,
		Content: []byte("cluster:\n  allowSchedulingOnControlPlanes: true\n"),
	}

	patches := make([]Patch, 0, 1+len(additionalPatches))
	patches = append(patches, allowSchedulingPatch)
	patches = append(patches, additionalPatches...)

	patches, err := migrateKubernetesPatchesForContract(patches, versionContract)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate Kubernetes patches: %w", err)
	}

	return newConfigs(
		DefaultClusterName,
		kubernetesVersion,
		DefaultNetworkCIDR,
		patches,
		versionContract,
	)
}

// NewDefaultConfigsWithVersionContractAndName builds the named default Talos
// bundle used by the operator and local provisioner. Its generated document
// shape matches the target Talos contract.
func NewDefaultConfigsWithVersionContractAndName(
	kubernetesVersion string,
	name string,
	versionContract *talosconfig.VersionContract,
) (*Configs, error) {
	configs, err := NewDefaultConfigsWithVersionContractAndPatches(
		kubernetesVersion,
		versionContract,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build talos config: %w", err)
	}

	named, err := configs.WithName(name)
	if err != nil {
		return nil, fmt.Errorf("name talos config: %w", err)
	}

	return named, nil
}

// Bundle returns the underlying Talos config bundle.
// This provides full access to all bundle functionality.
func (c *Configs) Bundle() *bundle.Bundle {
	return c.bundle
}

// ControlPlane returns the control-plane machine configuration.
// This config has cluster and control-plane patches applied.
//
// The returned config.Provider gives programmatic access to all config fields:
//   - Machine() - machine-specific settings (network, kubelet, files, etc.)
//   - Cluster() - cluster-wide settings (CNI, API server, etcd, etc.)
//
// Returns nil if the bundle is not loaded or if the control plane config is not set.
// This prevents panics from the upstream Talos SDK's bundle.ControlPlane() method
// which panics when ControlPlaneCfg is nil.
func (c *Configs) ControlPlane() talosconfig.Provider {
	if c.bundle == nil || c.bundle.ControlPlaneCfg == nil {
		return nil
	}

	return c.bundle.ControlPlane()
}

// Worker returns the worker machine configuration.
// This config has cluster and worker patches applied.
//
// The returned config.Provider gives programmatic access to all config fields:
//   - Machine() - machine-specific settings (network, kubelet, files, etc.)
//   - Cluster() - cluster-wide settings (CNI, API server, etcd, etc.)
//
// Returns nil if the bundle is not loaded or if the worker config is not set.
// This prevents panics from the upstream Talos SDK's bundle.Worker() method
// which panics when WorkerCfg is nil.
func (c *Configs) Worker() talosconfig.Provider {
	if c.bundle == nil || c.bundle.WorkerCfg == nil {
		return nil
	}

	return c.bundle.Worker()
}

// GetClusterName returns the cluster name.
// This implements configmanager.ClusterNameProvider interface.
func (c *Configs) GetClusterName() string {
	return c.Name
}

// WithName creates a new Configs with a different cluster name.
// This regenerates the underlying Talos bundle with the new cluster name,
// which is necessary because the cluster name is embedded in PKI certificates
// and the kubeconfig context name (admin@<cluster-name>).
//
// Returns a new Configs instance; the original is not modified.
// Returns an error if bundle regeneration fails.
func (c *Configs) WithName(name string) (*Configs, error) {
	if name == "" || name == c.Name {
		return c, nil
	}

	// Regenerate with the new cluster name. The PKI is intentionally regenerated
	// (secrets left nil) because the cluster name is embedded in the certificates.
	return c.regenerate(func(params *regenParams) error {
		params.name = name

		return nil
	})
}

// WithEndpoint creates a new Configs with a specific endpoint IP for the Talos API and Kubernetes API.
// This is used for cloud deployments (e.g., Hetzner) where the public IP is different from
// the internal network IP. The endpoint is embedded in certificates and must match the IP
// that clients will use to connect.
//
// The endpoint should be the public IP address of the first control plane node.
// Returns a new Configs instance; the original is not modified.
// Returns an error if bundle regeneration fails.
//
// IMPORTANT: This preserves the existing PKI (CA, certificates) to ensure that configs
// applied to servers and the talosconfig for authentication use the same CA.
func (c *Configs) WithEndpoint(endpointIP string) (*Configs, error) {
	if endpointIP == "" || endpointIP == c.endpoint {
		return c, nil
	}

	// Regenerate with the new endpoint, preserving the existing PKI so the configs
	// applied to servers and the talosconfig keep using the same CA.
	return c.regenerate(func(params *regenParams) error {
		params.endpoint = endpointIP

		return c.preserveSecrets(params)
	})
}

// WithCertSANs creates a new Configs whose Kubernetes API server and machine certificates include
// the given Subject Alternative Names, preserving the existing PKI. This is needed when the
// Kubernetes API is reached via a stable exposure address (NodePort/LoadBalancer/Gateway) that
// differs from the in-cluster addresses. Pass the full SAN set (e.g. loopback + exposure address),
// since the patch replaces the certSANs list rather than appending to it.
//
// Returns a new Configs instance; the original is not modified. Returns the original unchanged
// when sans is empty.
func (c *Configs) WithCertSANs(sans []string) (*Configs, error) {
	if len(sans) == 0 {
		return c, nil
	}

	// Append the cert-SANs patch and regenerate, preserving the existing PKI.
	return c.regenerate(func(params *regenParams) error {
		patches := make([]Patch, 0, len(c.patches)+1)
		patches = append(patches, c.patches...)
		patches = append(patches, buildCertSANsPatch(sans, c.versionContract))
		params.patches = patches

		return c.preserveSecrets(params)
	})
}

// buildCertSANsPatch builds a cluster-scope patch that sets the API server and
// machine certificate SANs. Talos 1.14 uses KubeAPIServerConfig for the API
// server values; older contracts use cluster.apiServer. The machine SANs remain
// in MachineConfig for both forms.
func buildCertSANsPatch(
	sans []string,
	versionContract *talosconfig.VersionContract,
) Patch {
	var sansList strings.Builder
	for _, san := range sans {
		_, _ = fmt.Fprintf(&sansList, "    - %q\n", san)
	}

	var machineSANs strings.Builder
	for _, san := range sans {
		_, _ = fmt.Fprintf(&machineSANs, "  - %q\n", san)
	}

	content := "cluster:\n  apiServer:\n    certSANs:\n" + sansList.String() +
		"machine:\n  certSANs:\n" + machineSANs.String()
	if versionContract != nil && versionContract.MultidocKubernetesConfigSupported() {
		content = "apiVersion: v1alpha1\nkind: KubeAPIServerConfig\ncertExtraSANs:\n" +
			sansList.String() + "---\nmachine:\n  certSANs:\n" + machineSANs.String()
	}

	return Patch{
		Path:    "ksail-exposure-cert-sans",
		Scope:   PatchScopeCluster,
		Content: []byte(content),
	}
}

// WithHetznerVIP creates a new Configs whose control-plane machine configs carry a Talos
// virtual-IP block for the given Hetzner floating IP, preserving the existing PKI. The
// elected etcd leader claims the address via the Hetzner Cloud API, so the cluster keeps
// answering on the floating IP across control-plane rolls without any user-provided patch.
//
// The hcloud API token is embedded in the rendered control-plane machine config — the
// trust surface the Talos hcloud VIP support prescribes. Returns the original unchanged
// when vip is empty; returns ErrHetznerVIPTokenRequired when vip is set but the token is
// empty (Talos cannot reassign the address without it).
func (c *Configs) WithHetznerVIP(vip, hcloudAPIToken string) (*Configs, error) {
	if vip == "" {
		return c, nil
	}

	if hcloudAPIToken == "" {
		return nil, ErrHetznerVIPTokenRequired
	}

	// Append the VIP patch and regenerate, preserving the existing PKI.
	return c.regenerate(func(params *regenParams) error {
		patches := make([]Patch, 0, len(c.patches)+1)
		patches = append(patches, c.patches...)
		patches = append(patches, buildHetznerVIPPatch(vip, hcloudAPIToken))
		params.patches = patches

		return c.preserveSecrets(params)
	})
}

// HetznerPublicNICBusPath is the PCI bus path of the public NIC on a Hetzner Cloud
// server. The public interface is always the first virtio device (slot 01); a cluster
// private network attaches as a later device (slot 07 on the two-NIC servers KSail
// provisions), so the bus path distinguishes the two where `physical: true` cannot.
//
// Exported because the update path reads it too: drift detection has to recognise a
// VIP that is present but addressed the OLD (name-based) way, so clusters built by an
// earlier release are migrated rather than reported as already configured. Keep the
// two uses in lockstep — they are the same contract seen from both ends.
const HetznerPublicNICBusPath = "0000:01:00.0"

// buildHetznerVIPPatch builds a control-plane-scope patch that configures the Talos
// virtual (shared) IP on the public interface, with hcloud API management so the
// elected leader reassigns the floating IP to itself.
//
// The public interface is addressed by a deviceSelector on its PCI bus path rather
// than by link name. Addressing it as `eth0` (the original design on
// devantler-tech/ksail#5718) assumed Talos uses ethN naming — it does not: Talos
// applies predictable interface names, so a Hetzner control plane enumerates its NICs
// as enp1s0 (public) and enp7s0 (private) and carries no `eth0` at all. The patch
// therefore matched no link, the VIP was never claimed on the node, and the floating
// IP stayed attached-but-unbound with a dead API endpoint (devantler-tech/ksail#6070).
// A `physical: true` selector cannot be used instead: KSail always attaches its servers
// to a cluster private network, so it would match both NICs. dhcp stays enabled so
// declaring the interface does not drop its primary address configuration.
func buildHetznerVIPPatch(vip, hcloudAPIToken string) Patch {
	content := fmt.Sprintf(
		"machine:\n"+
			"  network:\n"+
			"    interfaces:\n"+
			"      - deviceSelector:\n"+
			"          busPath: %q\n"+
			"        dhcp: true\n"+
			"        vip:\n"+
			"          ip: %q\n"+
			"          hcloud:\n"+
			"            apiToken: %q\n",
		HetznerPublicNICBusPath,
		vip,
		hcloudAPIToken,
	)

	return Patch{
		Path:    "ksail-hetzner-vip",
		Scope:   PatchScopeControlPlane,
		Content: []byte(content),
	}
}

// APIServerFeatureGatesPatch builds a cluster-scope patch that enables the
// MutatingAdmissionPolicy feature gate and the admissionregistration.k8s.io/v1beta1
// API on the kube-apiserver. Calico v3.30+ ships MutatingAdmissionPolicy resources in
// its CRD chart that require this API. Values carry no leading dashes. The config
// manager translates the legacy cluster.apiServer patch to KubeAPIServerConfig
// for Talos 1.14 contracts. Apply it only for clusters using the Calico CNI.
func APIServerFeatureGatesPatch() Patch {
	content := "cluster:\n" +
		"  apiServer:\n" +
		"    extraArgs:\n" +
		"      feature-gates: MutatingAdmissionPolicy=true\n" +
		"      runtime-config: admissionregistration.k8s.io/v1beta1=true\n"

	return Patch{
		Path:    "ksail-apiserver-feature-gates",
		Scope:   PatchScopeCluster,
		Content: []byte(content),
	}
}

// WithSecrets creates a new Configs with the provided secrets bundle, preserving the cluster's
// existing PKI (CA, certificates, tokens, bootstrap secrets) across config regeneration.
// This is used during cluster update to ensure that newly generated machine configs for
// scale-up operations use the same CA and tokens as the running cluster.
//
// Returns a new Configs instance; the original is not modified.
// Returns an error if bundle regeneration fails.
// Returns the original Configs unchanged if existingSecrets is nil.
func (c *Configs) WithSecrets(existingSecrets *secrets.Bundle) (*Configs, error) {
	if existingSecrets == nil {
		return c, nil
	}

	return c.regenerate(func(params *regenParams) error {
		params.secrets = existingSecrets

		return nil
	})
}

// WithKubernetesVersion creates a new Configs that targets the given Kubernetes
// version, regenerating the bundle so the kubelet, kube-apiserver,
// kube-controller-manager, and kube-scheduler image tags all match. The existing
// PKI is preserved so the regenerated config still aligns with a running cluster.
//
// This is used during cluster update to render the desired machine config at the
// Kubernetes version actually running on the cluster (rather than KSail's built-in
// default), so an unrelated update never proposes an unrequested Kubernetes
// upgrade. The version is normalised to drop any "v" prefix.
//
// Returns a new Configs instance; the original is not modified. Returns the
// original unchanged when version is empty or already matches.
func (c *Configs) WithKubernetesVersion(version string) (*Configs, error) {
	version = normalizeKubernetesVersion(version)
	if version == "" || version == c.kubernetesVersion {
		return c, nil
	}

	// Target the requested version while preserving the existing PKI so the
	// regenerated config keeps matching the running cluster.
	return c.regenerate(func(params *regenParams) error {
		params.kubernetesVersion = version

		return c.preserveSecrets(params)
	})
}

// ExtractSecrets extracts the secrets bundle from the current config for reuse.
// Returns nil if the bundle or control plane config is not set, or an error if
// the secrets bundle cannot be derived from the control plane config.
func (c *Configs) ExtractSecrets() (*secrets.Bundle, error) {
	if c.bundle == nil || c.bundle.ControlPlaneCfg == nil {
		return nil, nil //nolint:nilnil // nil bundle is a valid "not set" result, not an error
	}

	bundle, err := secrets.NewBundleFromConfig(
		secrets.NewFixedClock(time.Now()),
		c.bundle.ControlPlaneCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("extracting secrets bundle: %w", err)
	}

	return bundle, nil
}

// KubernetesVersion returns the Kubernetes version used for this config.
// Falls back to DefaultKubernetesVersion if not set.
func (c *Configs) KubernetesVersion() string {
	if c.kubernetesVersion != "" {
		return c.kubernetesVersion
	}

	return DefaultKubernetesVersion
}

// Patches returns a copy of the patches used to build this config.
// A copy is returned to prevent callers from mutating the internal state.
func (c *Configs) Patches() []Patch {
	if c.patches == nil {
		return nil
	}

	result := make([]Patch, len(c.patches))
	copy(result, c.patches)

	return result
}

// SchematicID returns the computed Talos Image Factory schematic ID.
// Returns an empty string if no extensions are configured.
func (c *Configs) SchematicID() string {
	return c.schematicID
}

// Extensions returns a copy of the configured extensions list.
func (c *Configs) Extensions() []string {
	if c.extensions == nil {
		return nil
	}

	result := make([]string, len(c.extensions))
	copy(result, c.extensions)

	return result
}

// InstallImagePatch reports whether any user-provided patch sets machine.install.image,
// returning the effective value. KSail derives machine.install.image from
// spec.cluster.talos.extensions (see applySchematic), so a patch that also sets it is
// redundant and silently overridden during config generation — callers use this to warn
// about the resulting skew. The raw patch content is inspected (not the already-overridden
// bundle), so the user's intent is recovered rather than KSail's computed value.
//
// Patches are stored in application order (LoadPatches: cluster, control-plane, worker;
// then runtime patches appended) and a later strategic-merge patch overrides an earlier
// one, so the LAST patch that sets the image is the effective value. Only strategic-merge
// patches are detected; an RFC-6902 op targeting /machine/install/image is not (none are
// scaffolded, and such a patch is unusual).
func (c *Configs) InstallImagePatch() (string, bool) {
	image := ""

	for _, patch := range c.patches {
		// Track field presence, not string-emptiness: the LAST patch that sets the
		// machine.install.image field wins, even when it explicitly clears the image
		// to "" (an earlier patch's value must not then be reported as effective).
		if patchImage, present := installImageFromPatch(patch.Content); present {
			image = patchImage
		}
	}

	// found is true only when the effective value is a real (non-empty) image — an
	// explicit clear leaves no effective pin, so no skew warning is warranted.
	return image, image != ""
}

// installImageFromPatch extracts machine.install.image from a strategic-merge patch's raw
// YAML content. The bool reports whether the patch sets the machine.install.image field at
// all (independent of the string value, so an explicit empty clear is distinguishable from
// an absent field). Returns ("", false) when the patch does not set it, or when the content
// is not a strategic-merge document (e.g. an RFC-6902 patch list).
func installImageFromPatch(content []byte) (string, bool) {
	var doc struct {
		Machine *struct {
			Install *struct {
				Image *string `json:"image"`
			} `json:"install"`
		} `json:"machine"`
	}

	err := yaml.Unmarshal(content, &doc)
	if err != nil {
		return "", false
	}

	if doc.Machine == nil || doc.Machine.Install == nil || doc.Machine.Install.Image == nil {
		return "", false
	}

	return strings.TrimSpace(*doc.Machine.Install.Image), true
}

// IsCNIDisabled returns true if the default CNI is disabled (set to "none").
// This is used to determine whether to skip CNI-dependent checks during bootstrap.
//
// In the Talos alpha.2 multi-document config model the top-level CNI accessor was
// removed: Flannel is the only built-in CNI, exposed as a K8sFlannelCNIConfig
// document, and the machinery bridge returns a nil K8sFlannelCNIConfig whenever the
// CNI name is not "flannel" (i.e. it is "none"). ksail only ever generates the
// default Flannel or, via its disable-default-cni patch, "none", so a nil
// K8sFlannelCNIConfig is exactly the "CNI disabled" case. The K8sNetworkConfig guard
// preserves the previous behaviour of returning false when the config carries no
// cluster-network section at all (undeterminable rather than disabled).
func (c *Configs) IsCNIDisabled() bool {
	cp := c.ControlPlane()
	if cp == nil || cp.K8sNetworkConfig() == nil {
		return false
	}

	return cp.K8sFlannelCNIConfig() == nil
}

// IsKubeletCertRotationEnabled returns true if kubelet serving certificate rotation is enabled.
// This is detected by the presence of "rotate-server-certificates" in kubelet extra args.
// When enabled with CNI disabled, the kubelet-serving-cert-approver pod cannot schedule
// (node is NotReady without CNI), so kubelet has no serving certificate, and Talos cannot
// connect to kubelet to populate StaticPodStatus resources.
func (c *Configs) IsKubeletCertRotationEnabled() bool {
	cp := c.ControlPlane()
	if cp == nil || cp.Machine() == nil || cp.Machine().Kubelet() == nil {
		return false
	}

	extraArgs := cp.Machine().Kubelet().ExtraArgs()
	if extraArgs == nil {
		return false
	}

	val, ok := extraArgs["rotate-server-certificates"]
	if !ok {
		return false
	}

	// ExtraArgs now returns map[string][]string (Talos SDK v1.13.0-alpha.1+)
	// Check if "true" is present in the slice
	return slices.Contains(val, "true")
}

// ExtractMirrorHosts returns a list of registry hosts that have mirror configurations.
// This extracts hosts from the loaded config bundle, which includes any patches that
// were applied (including scaffolded mirror-registries.yaml patches).
// Returns nil if no mirrors are configured.
//
// Note: This method only returns host names, not remote URLs. For full MirrorSpec
// extraction including remotes, parsing from the generator-created patch files is
// needed, or use DefaultGenerateUpstreamURL to derive conventional upstream URLs.
func (c *Configs) ExtractMirrorHosts() []string {
	cp := c.ControlPlane()
	if cp == nil {
		return nil
	}

	mirrors := cp.RegistryMirrorConfigs()
	if len(mirrors) == 0 {
		return nil
	}

	hosts := make([]string, 0, len(mirrors))
	for host := range mirrors {
		hosts = append(hosts, host)
	}

	return hosts
}

// NetworkCIDR returns the network CIDR from the cluster configuration.
// This is extracted from the pod CIDRs in the cluster network settings.
//
// In the Talos alpha.2 multi-document config model the pod CIDRs live on the
// K8sNetworkConfig document and are typed as netip.Prefix rather than string, so the
// first entry is rendered back with String() to preserve the previous "10.244.0.0/16"
// form.
func (c *Configs) NetworkCIDR() string {
	cp := c.ControlPlane()
	if cp == nil || cp.K8sNetworkConfig() == nil {
		return DefaultNetworkCIDR
	}

	podCIDRs := cp.K8sNetworkConfig().PodCIDRs()
	if len(podCIDRs) > 0 {
		return podCIDRs[0].String()
	}

	return DefaultNetworkCIDR
}

// newConfigs creates Configs from patches with the given cluster parameters.
// The endpoint is calculated from the network CIDR.
// If versionContract is nil, it defaults to TalosVersion1_12 for compatibility with Hetzner
// bootstrap ISOs.
func newConfigs(
	clusterName string,
	kubernetesVersion string,
	networkCIDR string,
	patches []Patch,
	versionContract *talosconfig.VersionContract,
) (*Configs, error) {
	return newConfigsWithExtensions(
		clusterName,
		kubernetesVersion,
		networkCIDR,
		patches,
		versionContract,
		nil,
	)
}

// newConfigsWithExtensions creates Configs from patches and optional extensions.
func newConfigsWithExtensions(
	clusterName string,
	kubernetesVersion string,
	networkCIDR string,
	patches []Patch,
	versionContract *talosconfig.VersionContract,
	extensions []string,
) (*Configs, error) {
	return newConfigsWithEndpointAndSecrets(
		clusterName,
		kubernetesVersion,
		networkCIDR,
		"",
		patches,
		nil,
		versionContract,
		extensions,
	)
}

// resolveControlPlaneIP determines the control plane IP from explicit endpoint or network CIDR.
func resolveControlPlaneIP(endpointIP, networkCIDR string) (string, error) {
	if endpointIP != "" {
		return endpointIP, nil
	}

	// Parse network CIDR for endpoint calculation
	parsedCIDR, parseErr := netip.ParsePrefix(networkCIDR)
	if parseErr != nil {
		return "", fmt.Errorf("invalid network CIDR: %w", parseErr)
	}

	// Calculate control plane IP for endpoint
	ipAddr, ipErr := nthIPInNetwork(parsedCIDR, ipv4Offset)
	if ipErr != nil {
		return "", fmt.Errorf("failed to calculate control plane IP: %w", ipErr)
	}

	return ipAddr.String(), nil
}

// buildBaseGenOptions creates the base generate options for Talos config generation.
// If versionContract is nil, it defaults to TalosVersion1_12.
//
// TalosVersion1_12 is the conservative default because the Hetzner bootstrap ISO
// (ID 125127) runs Talos 1.12.4 in maintenance mode. Version contracts greater than 1.12
// generate fields unknown to the 1.12.4 machined, causing config apply to fail with
// "unknown keys found during decoding". (Note: machine.install.grubUseUKICmdline is
// gated at >1.11, so the 1.12 default already emits it — it is not an example of a
// post-1.12 field.) Update this default when Hetzner publishes a newer Talos bootstrap
// ISO (and bump DefaultTalosISO in pkg/apis/cluster/v1alpha1 to match).
func buildBaseGenOptions(
	controlPlaneIP string,
	versionContract *talosconfig.VersionContract,
) []generate.Option {
	if versionContract == nil {
		versionContract = talosconfig.TalosVersion1_12
	}

	return []generate.Option{
		generate.WithEndpointList([]string{controlPlaneIP}),
		generate.WithAdditionalSubjectAltNames([]string{"127.0.0.1"}),
		generate.WithVersionContract(versionContract),
		// Install disk is required for bare metal installations (Hetzner, etc.)
		// For Docker-in-Docker, this setting is ignored as there's no actual disk.
		// /dev/sda is the standard disk for Hetzner VPS and most cloud providers.
		generate.WithInstallDisk("/dev/sda"),
	}
}

// buildBundleOptions creates bundle options with patches applied.
func buildBundleOptions(
	clusterName string,
	controlPlaneEndpoint string,
	kubernetesVersion string,
	genOptions []generate.Option,
	clusterPatches, controlPlanePatches, workerPatches []configpatcher.Patch,
) []bundle.Option {
	bundleOpts := []bundle.Option{
		bundle.WithInputOptions(&bundle.InputOptions{
			ClusterName: clusterName,
			Endpoint:    controlPlaneEndpoint,
			KubeVersion: kubernetesVersion,
			GenOptions:  genOptions,
		}),
		bundle.WithVerbose(false), // Suppress "generating PKI and tokens" output
	}

	// Add patches by scope
	if len(clusterPatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatch(clusterPatches))
	}

	if len(controlPlanePatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatchControlPlane(controlPlanePatches))
	}

	if len(workerPatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatchWorker(workerPatches))
	}

	return bundleOpts
}

// newConfigsWithEndpointAndSecrets creates Configs with an explicit endpoint IP while preserving
// an existing secrets bundle. This is used by WithEndpoint to regenerate configs with a new
// endpoint without regenerating the PKI (CA, keys, tokens), which would cause certificate mismatches.
// If versionContract is nil, it defaults to TalosVersion1_12.
func newConfigsWithEndpointAndSecrets(
	clusterName string,
	kubernetesVersion string,
	networkCIDR string,
	endpointIP string,
	patches []Patch,
	existingSecrets *secrets.Bundle,
	versionContract *talosconfig.VersionContract,
	extensions []string,
) (*Configs, error) {
	// Default nil versionContract so the stored and generated values always match.
	if versionContract == nil {
		versionContract = talosconfig.TalosVersion1_12
	}

	clusterPatches, controlPlanePatches, workerPatches, err := categorizePatchesByScope(patches)
	if err != nil {
		return nil, err
	}

	controlPlaneIP, err := resolveControlPlaneIP(endpointIP, networkCIDR)
	if err != nil {
		return nil, err
	}

	genOptions := buildBaseGenOptions(controlPlaneIP, versionContract)
	if existingSecrets != nil {
		genOptions = append(genOptions, generate.WithSecretsBundle(existingSecrets))
	}

	bundleOpts := buildBundleOptions(
		clusterName,
		"https://"+net.JoinHostPort(controlPlaneIP, "6443"),
		kubernetesVersion,
		genOptions,
		clusterPatches,
		controlPlanePatches,
		workerPatches,
	)

	configBundle, err := bundle.NewBundle(bundleOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create config bundle: %w", err)
	}

	schematicID, err := applySchematic(extensions, configBundle)
	if err != nil {
		return nil, err
	}

	return &Configs{
		Name:              clusterName,
		bundle:            configBundle,
		kubernetesVersion: kubernetesVersion,
		networkCIDR:       networkCIDR,
		endpoint:          endpointIP,
		patches:           patches,
		versionContract:   versionContract,
		extensions:        extensions,
		schematicID:       schematicID,
	}, nil
}

// MirrorRegistry represents a registry mirror configuration.
type MirrorRegistry struct {
	// Host is the registry host (e.g., "docker.io", "ghcr.io").
	Host string
	// Endpoints are the mirror endpoints to use (e.g., "http://docker.io:5000").
	Endpoints []string
	// Username is the optional username for registry authentication.
	// Environment variable placeholders should be resolved before passing.
	Username string
	// Password is the optional password for registry authentication.
	// Environment variable placeholders should be resolved before passing.

	Password string
}

// ApplyMirrorRegistries modifies the configs to add registry mirror configurations.
// This directly patches the underlying v1alpha1.Config structs.
// It adds both the mirror endpoints and the registry config with insecureSkipVerify: true
// to allow HTTP connections to local registry containers.
func (c *Configs) ApplyMirrorRegistries(mirrors []MirrorRegistry) error {
	if len(mirrors) == 0 || c.bundle == nil {
		return nil
	}

	// Define the patcher function that adds mirrors and registry configs
	patcher := func(cfg *v1alpha1.Config) error {
		return applyMirrorsToConfig(cfg, mirrors)
	}

	// Apply to control plane config
	if c.bundle.ControlPlaneCfg != nil {
		patched, err := c.bundle.ControlPlaneCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to patch control plane config: %w", err)
		}

		c.bundle.ControlPlaneCfg = patched
	}

	// Apply to worker config
	if c.bundle.WorkerCfg != nil {
		patched, err := c.bundle.WorkerCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to patch worker config: %w", err)
		}

		c.bundle.WorkerCfg = patched
	}

	return nil
}

// regenParams bundles every input needed to regenerate a Configs bundle. It
// replaces the 8-positional-parameter newConfigsWithEndpointAndSecrets call that
// each With* method threaded by hand, so a new regeneration variant mutates a
// named field instead of re-spelling the whole argument list.
type regenParams struct {
	name              string
	kubernetesVersion string
	networkCIDR       string
	endpoint          string
	patches           []Patch
	secrets           *secrets.Bundle
	versionContract   *talosconfig.VersionContract
	extensions        []string
}

// snapshot captures the current Configs state as regenParams with the
// kubernetesVersion/networkCIDR fallbacks applied once, so every With* method
// stops re-deriving them. Secrets default to nil (full PKI regeneration); callers
// that must preserve PKI set params.secrets via preserveSecrets in their mutate.
func (c *Configs) snapshot() regenParams {
	kubernetesVersion := c.kubernetesVersion
	if kubernetesVersion == "" {
		kubernetesVersion = DefaultKubernetesVersion
	}

	networkCIDR := c.networkCIDR
	if networkCIDR == "" {
		networkCIDR = DefaultNetworkCIDR
	}

	return regenParams{
		name:              c.Name,
		kubernetesVersion: kubernetesVersion,
		networkCIDR:       networkCIDR,
		endpoint:          c.endpoint,
		patches:           c.patches,
		secrets:           nil,
		versionContract:   c.versionContract,
		extensions:        c.extensions,
	}
}

// regenerate rebuilds the bundle from a snapshot of the current state after
// applying mutate. It is the single regeneration path shared by every With*
// method (and replaces the per-method newConfigsWithEndpointAndSecrets calls).
// mutate may return an error (e.g. when preserving PKI fails), which aborts the
// regeneration.
func (c *Configs) regenerate(mutate func(*regenParams) error) (*Configs, error) {
	params := c.snapshot()

	err := mutate(&params)
	if err != nil {
		return nil, err
	}

	return newConfigsWithEndpointAndSecrets(
		params.name,
		params.kubernetesVersion,
		params.networkCIDR,
		params.endpoint,
		params.patches,
		params.secrets,
		params.versionContract,
		params.extensions,
	)
}

// preserveSecrets extracts the current PKI into params.secrets so regeneration
// keeps the running cluster's CA, certificates, tokens, and bootstrap secrets.
// It centralizes the secrets-extraction the WithEndpoint/WithCertSANs/
// WithKubernetesVersion methods used to inline verbatim.
func (c *Configs) preserveSecrets(params *regenParams) error {
	existing, err := c.ExtractSecrets()
	if err != nil {
		return err
	}

	params.secrets = existing

	return nil
}

// applyMirrorsToConfig applies mirror configurations to a Talos v1alpha1 config.
func applyMirrorsToConfig(cfg *v1alpha1.Config, mirrors []MirrorRegistry) error {
	if cfg.MachineConfig == nil {
		return nil
	}

	initRegistryMaps(cfg)

	for _, mirror := range mirrors {
		if mirror.Host == "" {
			continue
		}

		addMirrorEndpoints(cfg, mirror)

		// Add authentication if credentials are provided
		if mirror.Username != "" || mirror.Password != "" {
			addRegistryAuth(cfg, mirror)
		}
		// NOTE: We intentionally do NOT call addInsecureRegistryConfigs for HTTP endpoints.
		// containerd will reject TLS configuration for non-HTTPS registries with the error:
		// "TLS config specified for non-HTTPS registry"
		// HTTP endpoints work without any additional configuration.
	}

	return nil
}

// initRegistryMaps initializes the registry maps if they are nil.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func initRegistryMaps(cfg *v1alpha1.Config) {
	if cfg.MachineConfig.MachineRegistries.RegistryMirrors == nil {
		cfg.MachineConfig.MachineRegistries.RegistryMirrors = make(
			map[string]*v1alpha1.RegistryMirrorConfig,
		)
	}

	if cfg.MachineConfig.MachineRegistries.RegistryConfig == nil {
		cfg.MachineConfig.MachineRegistries.RegistryConfig = make(
			map[string]*v1alpha1.RegistryConfig,
		)
	}
}

// addMirrorEndpoints adds mirror endpoint configuration for a registry host.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func addMirrorEndpoints(cfg *v1alpha1.Config, mirror MirrorRegistry) {
	cfg.MachineConfig.MachineRegistries.RegistryMirrors[mirror.Host] = &v1alpha1.RegistryMirrorConfig{
		MirrorEndpoints: mirror.Endpoints,
	}
}

// addRegistryAuth adds authentication configuration for a registry host.
// This configures machine.registries.config.<host>.auth with username and password.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func addRegistryAuth(cfg *v1alpha1.Config, mirror MirrorRegistry) {
	// Ensure the registry config exists for this host
	if cfg.MachineConfig.MachineRegistries.RegistryConfig[mirror.Host] == nil {
		cfg.MachineConfig.MachineRegistries.RegistryConfig[mirror.Host] = &v1alpha1.RegistryConfig{}
	}

	// Set the auth configuration
	cfg.MachineConfig.MachineRegistries.RegistryConfig[mirror.Host].RegistryAuth = &v1alpha1.RegistryAuthConfig{
		RegistryUsername: mirror.Username,
		RegistryPassword: mirror.Password,
	}
}

// applySchematic computes a schematic ID from extensions and patches machine.install.image.
// Returns an empty string if no extensions are configured (after normalization).
//
// Any machine.install.extraKernelArgs the patches set on the (already generated) config are
// folded into the Image Factory schematic — baked into the installer image alongside the
// extensions, so they apply consistently to the static install image, the Hetzner autoscaler
// snapshot, and the rolling-upgrade installer — and then cleared from the rendered config,
// which is simultaneously pinned to install.grubUseUKICmdline=true so the now-UKI-embedded args
// actually take effect (false would make GRUB rebuild the cmdline host-side and silently drop
// them). This keeps kernel args declared in native Talos patch files while moving off the
// deprecated machine.install.extraKernelArgs field, and avoids the Talos >=1.13.4 rejection of
// that field alongside install.grubUseUKICmdline. Folding is gated on extensions being
// configured, the same signal that already selects a factory installer (so container-mode
// Docker clusters, which use no factory installer, are unaffected).
func applySchematic(extensions []string, configBundle *bundle.Bundle) (string, error) {
	normalized := NormalizeExtensions(extensions)
	if len(normalized) == 0 {
		return "", nil
	}

	kernelArgs := schematicKernelArgs(configBundle)

	schematic := NewSchematic(extensions, kernelArgs)

	schematicID, err := schematic.ID()
	if err != nil {
		return "", fmt.Errorf("failed to compute schematic ID: %w", err)
	}

	talosVersion := resolveInstallerVersion(configBundle)
	installerImage := SchematicInstallerImage(schematicID, talosVersion)

	err = applyInstallerImage(configBundle, installerImage)
	if err != nil {
		return "", fmt.Errorf("failed to apply installer image: %w", err)
	}

	if len(kernelArgs) > 0 {
		err = reconcileFoldedKernelArgs(configBundle)
		if err != nil {
			return "", fmt.Errorf("failed to reconcile folded kernel args: %w", err)
		}
	}

	return schematicID, nil
}

// schematicKernelArgs returns the deduplicated union of machine.install.extraKernelArgs
// across the control-plane and worker configs. A cluster-scope patch sets the same args on
// both; a control-plane- or worker-scope patch sets them on one. These are folded into the
// Image Factory schematic and then reconciled out of the rendered config by
// reconcileFoldedKernelArgs.
func schematicKernelArgs(configBundle *bundle.Bundle) []string {
	seen := make(map[string]struct{})

	var args []string

	for _, provider := range []talosconfig.Provider{
		configBundle.ControlPlane(),
		configBundle.Worker(),
	} {
		if provider == nil || provider.Machine() == nil || provider.Machine().Install() == nil {
			continue
		}

		for _, arg := range provider.Machine().Install().ExtraKernelArgs() {
			arg = strings.TrimSpace(arg)
			if arg == "" {
				continue
			}

			if _, ok := seen[arg]; ok {
				continue
			}

			seen[arg] = struct{}{}
			args = append(args, arg)
		}
	}

	return args
}

// reconcileFoldedKernelArgs reconciles the control-plane and worker configs after their
// machine.install.extraKernelArgs have been folded into the Image Factory schematic. It (a)
// clears the deprecated machine.install.extraKernelArgs field (which Talos >=1.13.4 rejects
// together with install.grubUseUKICmdline) and (b) pins install.grubUseUKICmdline=true so the
// args — now embedded in the installer image's UKI cmdline — actually apply. Pinning true is
// what makes the migration safe regardless of what the patches set: a patch may carry
// grubUseUKICmdline=false (e.g. a pre-fold workaround that let the host-built cmdline include
// the args), but once the args live in the UKI cmdline, false would make GRUB rebuild the
// cmdline host-side and silently drop them — so it must become true.
func reconcileFoldedKernelArgs(configBundle *bundle.Bundle) error {
	useUKICmdline := true

	patcher := func(cfg *v1alpha1.Config) error {
		if cfg.MachineConfig == nil || cfg.MachineConfig.MachineInstall == nil {
			return nil
		}

		// Clear the deprecated field (folded into the schematic) and use the UKI cmdline
		// so the folded args apply.
		cfg.MachineConfig.MachineInstall.InstallExtraKernelArgs = nil //nolint:staticcheck
		cfg.MachineConfig.MachineInstall.InstallGrubUseUKICmdline = &useUKICmdline

		return nil
	}

	if configBundle.ControlPlaneCfg != nil {
		patched, err := configBundle.ControlPlaneCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to reconcile control plane folded kernel args: %w", err)
		}

		configBundle.ControlPlaneCfg = patched
	}

	if configBundle.WorkerCfg != nil {
		patched, err := configBundle.WorkerCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to reconcile worker folded kernel args: %w", err)
		}

		configBundle.WorkerCfg = patched
	}

	return nil
}

// resolveInstallerVersion determines the Talos version tag for the factory installer image.
// It extracts the tag from the config bundle's existing machine.install.image (set during
// bundle generation to match the configured Talos version). Falls back to DefaultTalosImage.
func resolveInstallerVersion(configBundle *bundle.Bundle) string {
	if image := controlPlaneInstallImage(configBundle); image != "" {
		if tag := extractImageTag(image); tag != "" {
			return tag
		}
	}

	if tag := extractImageTag(DefaultTalosImage); tag != "" {
		return tag
	}

	return "latest"
}

// extractImageTag extracts the tag from an OCI image reference.
// Strips any @digest suffix first, then extracts the tag after the last ":"
// only if it does not contain "/" (which would indicate a port, not a tag).
func extractImageTag(image string) string {
	if digestIdx := strings.Index(image, "@"); digestIdx >= 0 {
		image = image[:digestIdx]
	}

	if colonIdx := strings.LastIndex(image, ":"); colonIdx >= 0 {
		candidate := image[colonIdx+1:]
		if !strings.Contains(candidate, "/") {
			return candidate
		}
	}

	return ""
}

// controlPlaneInstallImage returns the control plane machine.install.image, or empty string.
func controlPlaneInstallImage(configBundle *bundle.Bundle) string {
	if configBundle == nil {
		return ""
	}

	cp := configBundle.ControlPlane()
	if cp == nil || cp.Machine() == nil || cp.Machine().Install() == nil {
		return ""
	}

	return cp.Machine().Install().Image()
}

// applyInstallerImage patches machine.install.image on both control-plane and worker configs.
func applyInstallerImage(configBundle *bundle.Bundle, installerImage string) error {
	patcher := func(cfg *v1alpha1.Config) error {
		if cfg.MachineConfig == nil {
			return nil
		}

		if cfg.MachineConfig.MachineInstall == nil {
			cfg.MachineConfig.MachineInstall = &v1alpha1.InstallConfig{}
		}

		cfg.MachineConfig.MachineInstall.InstallImage = installerImage

		return nil
	}

	if configBundle.ControlPlaneCfg != nil {
		patched, err := configBundle.ControlPlaneCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to patch control plane install image: %w", err)
		}

		configBundle.ControlPlaneCfg = patched
	}

	if configBundle.WorkerCfg != nil {
		patched, err := configBundle.WorkerCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to patch worker install image: %w", err)
		}

		configBundle.WorkerCfg = patched
	}

	return nil
}

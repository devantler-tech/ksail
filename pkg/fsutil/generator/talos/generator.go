package talosgenerator

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos/csrapprover"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

const (
	// dirPerm is the permission mode for created directories.
	dirPerm = 0o750
	// filePerm is the permission mode for created files.
	filePerm = 0o600
	// mirrorRegistriesFileName is the name of the generated mirror registries patch file.
	mirrorRegistriesFileName = "mirror-registries.yaml"
	// allowSchedulingFileName is the name of the control-plane scheduling patch file.
	allowSchedulingFileName = "allow-scheduling-on-control-planes.yaml"
	// disableCNIFileName is the name of the CNI disable patch file.
	disableCNIFileName = "disable-default-cni.yaml"
	// kubeletCertRotationFileName is the name of the kubelet certificate rotation patch file.
	kubeletCertRotationFileName = "kubelet-cert-rotation.yaml"
	// kubeletCSRApproverFileName is the name of the kubelet CSR approver extraManifest patch file.
	kubeletCSRApproverFileName = "kubelet-csr-approver.yaml"
	// clusterNameFileName is the name of the cluster name patch file.
	clusterNameFileName = "cluster-name.yaml"
	// imageVerificationFileName is the name of the image verification config document file.
	imageVerificationFileName = "image-verification.yaml"
	// disableCDIFileName is the name of the CDI disable patch file.
	disableCDIFileName = "disable-cdi.yaml"
	// externalCloudProviderFileName is the name of the external cloud provider patch file.
	externalCloudProviderFileName = "external-cloud-provider.yaml"
	// ingressFirewallDefaultActionFileName is the name of the ingress firewall default action document file.
	ingressFirewallDefaultActionFileName = "ingress-firewall-default-action.yaml"
	// ingressFirewallRulesFileName is the name of the ingress firewall rules document file.
	ingressFirewallRulesFileName = "ingress-firewall-rules.yaml"
	// oidcFileName is the name of the OIDC API server configuration patch file.
	oidcFileName = "oidc.yaml"
)

// ExternalCloudProviderPatchYAML is the Talos machine config patch YAML that enables
// the external cloud provider. This is the single source of truth for the patch content,
// shared between the generator (file-based scaffolding) and the runtime config manager
// (in-memory patch injection for existing projects).
//
// It enables both the cluster-level externalCloudProvider and the kubelet cloud-provider
// flag, which are required for cloud controller managers (e.g., Hetzner CCM) to initialize
// nodes with a providerID and write node labels.
//
// See: https://www.talos.dev/latest/kubernetes-guides/configuration/cloud-provider/
const ExternalCloudProviderPatchYAML = `cluster:
  externalCloudProvider:
    enabled: true
machine:
  kubelet:
    extraArgs:
      cloud-provider: external
`

// imageVerificationTemplate is the Talos ImageVerificationConfig document body
// (Talos 1.13+). Placed in cluster/ alongside other patches; Talos configpatcher
// recognizes it as a registered config document (kind: ImageVerificationConfig)
// and StrategicMerge appends it to the bundle rather than overwriting the
// MachineConfig.
// See: https://docs.siderolabs.com/talos/v1.13/security/verifying-image-signatures
const imageVerificationTemplate = `# Talos ImageVerificationConfig (Talos 1.13+)
# This document enables machine-wide container image signature verification.
# Rules are evaluated in order; the first matching rule applies.
# See: https://www.talos.dev/v1.13/talos-guides/configuration/image-verification/
apiVersion: v1alpha1
kind: ImageVerificationConfig
rules:
  # Default: skip verification for all images.
  # Remove or modify this rule and add specific verification rules below.
  - image: "*"
    skip: true
  # Example: Verify registry.k8s.io images using keyless (Cosign/OIDC) verification
  # - image: "registry.k8s.io/*"
  #   keyless:
  #     issuer: "https://accounts.google.com"
  #     subject: "krel-trust@k8s-releng-prod.iam.gserviceaccount.com"
  # Example: Verify images from a private registry using a public key
  # - image: "my-registry.example.com/*"
  #   publicKey:
  #     certificate: |
  #       -----BEGIN CERTIFICATE-----
  #       <your PEM-encoded certificate here>
  #       -----END CERTIFICATE-----
  # Example: Deny all images from an untrusted registry
  # - image: "untrusted-registry.example.com/*"
  #   deny: true
`

// ErrConfigRequired is returned when a nil config is provided.
var ErrConfigRequired = errors.New("talos config is required")

// Config represents the Talos scaffolding configuration.
type Config struct {
	// PatchesDir is the root directory for Talos patches.
	PatchesDir string
	// MirrorRegistries contains mirror registry specifications in "host=upstream" format.
	// Example: ["docker.io=https://registry-1.docker.io"]
	MirrorRegistries []string
	// WorkerNodes is the number of worker nodes configured.
	// When 0 (default), generates allow-scheduling-on-control-planes.yaml.
	WorkerNodes int
	// DisableDefaultCNI indicates whether to disable Talos's default CNI (Flannel).
	// When true, generates a version-appropriate disable-default-cni.yaml patch.
	// This is required when using an alternative CNI like Cilium.
	DisableDefaultCNI bool
	// EnableKubeletCertRotation indicates whether to enable kubelet serving certificate rotation.
	// When true, generates a kubelet-cert-rotation.yaml patch with rotate-server-certificates: true.
	// This is required for secure metrics-server communication using TLS.
	EnableKubeletCertRotation bool
	// ClusterName is an optional explicit cluster name override.
	// When set, generates a cluster-name.yaml patch to set cluster.clusterName.
	// This name is used for the kubeconfig context (admin@<name>).
	ClusterName string
	// EnableImageVerification indicates whether to generate an ImageVerificationConfig template.
	// When true, generates an image-verification.yaml document with a default skip-all rule
	// and commented examples for keyless (Cosign/OIDC) and public key verification.
	// This requires Talos 1.13+.
	EnableImageVerification bool
	// DisableCDI indicates whether to generate a patch that disables CDI.
	// When true, generates a disable-cdi.yaml patch to set machine.features.enableCDI to false.
	// Talos 1.13+ enables CDI by default, so this patch is only needed when CDI should be turned off.
	DisableCDI bool
	// EnableExternalCloudProvider indicates whether to enable the external cloud provider.
	// When true, generates an external-cloud-provider.yaml patch that sets
	// cluster.externalCloudProvider.enabled to true and machine.kubelet.extraArgs.cloud-provider
	// to "external". This is required for Hetzner Cloud so that the Cloud Controller Manager
	// can initialize nodes with a providerID and the CSI driver can schedule.
	// See: https://www.talos.dev/latest/kubernetes-guides/configuration/cloud-provider/
	EnableExternalCloudProvider bool
	// EnableIngressFirewall indicates whether to generate Talos ingress firewall documents.
	// When true, generates NetworkDefaultActionConfig (ingress: block) and per-role
	// NetworkRuleConfig documents for defense-in-depth at the OS level.
	// Requires the NetworkCIDR and CNIPort fields to be set.
	// See: https://www.talos.dev/latest/talos-guides/network/ingress-firewall/
	EnableIngressFirewall bool
	// NetworkCIDR is the cluster's private network CIDR, used to restrict
	// ingress firewall rules to cluster-internal traffic (e.g., "10.0.0.0/16").
	NetworkCIDR string
	// CNIPort is the CNI encapsulation port (e.g., 8472 for Cilium VXLAN, 4789 for Flannel/Calico).
	CNIPort int
	// AllowedCIDRs restricts the Kubernetes API and Talos API ingress firewall rules on
	// control-plane nodes to the specified CIDR blocks. When empty, those rules allow
	// 0.0.0.0/0 and ::/0 (open to all). Only affects the CP rules; worker rules are
	// always restricted to the private network CIDR.
	AllowedCIDRs []string
	// EnableOIDC indicates whether to generate an OIDC API server configuration patch.
	// Talos 1.14 uses KubeAuthenticationConfig; older releases use API-server flags.
	EnableOIDC bool
	// OIDCIssuerURL is the OIDC provider's issuer URL.
	OIDCIssuerURL string
	// OIDCClientID is the OIDC client ID.
	OIDCClientID string
	// OIDCUsernameClaim is the JWT claim for the Kubernetes username.
	OIDCUsernameClaim string
	// OIDCUsernamePrefix is the prefix for OIDC usernames.
	OIDCUsernamePrefix string
	// OIDCGroupsClaim is the JWT claim for Kubernetes groups.
	OIDCGroupsClaim string
	// OIDCGroupsPrefix is the prefix for OIDC groups.
	OIDCGroupsPrefix string
	// OIDCCAFile is the path to the CA certificate for self-signed OIDC providers.
	OIDCCAFile string
	// MultiDocumentKubernetesConfig generates the Talos 1.14 Kubernetes
	// configuration resources instead of the legacy cluster fields. Enable this
	// only when the config manager uses a matching Talos version contract.
	MultiDocumentKubernetesConfig bool
}

// Generator generates the Talos directory structure.
type Generator struct{}

// NewGenerator creates a new Generator.
func NewGenerator() *Generator {
	return &Generator{}
}

// Generate creates the Talos patches directory structure.
// The model parameter contains the patches directory path.
// Returns the generated directory path and any error encountered.
func (g *Generator) Generate(
	model *Config,
	opts yamlgenerator.Options,
) (string, error) {
	if model == nil {
		return "", ErrConfigRequired
	}

	baseDir := opts.Output
	if baseDir == "" {
		baseDir = "."
	}

	patchesDir := model.PatchesDir
	if patchesDir == "" {
		patchesDir = "talos"
	}

	rootPath := filepath.Join(baseDir, patchesDir)

	// Validate (and normalize) the ingress firewall model once up front when it is
	// enabled, since the per-file content functions in the patchSpecs table assume
	// NetworkCIDR/CNIPort are present and valid.
	if model.EnableIngressFirewall {
		err := validateIngressFirewallModel(model)
		if err != nil {
			return "", err
		}
	}

	// Determine which subdirectories will have patches generated
	dirsWithPatches := g.getDirectoriesWithPatches(model)

	// Create subdirectories, only adding .gitkeep to empty ones
	err := g.createSubdirectories(rootPath, dirsWithPatches, opts.Force)
	if err != nil {
		return "", err
	}

	// Generate conditional patches based on configuration
	err = g.generateConditionalPatches(rootPath, model, opts.Force)
	if err != nil {
		return "", err
	}

	return rootPath, nil
}

// getDirectoriesWithPatches returns a set of subdirectory names that will have
// patches generated, derived from the single patchSpecs table so it can never
// drift from the patches actually written.
func (g *Generator) getDirectoriesWithPatches(
	model *Config,
) map[string]bool {
	dirs := make(map[string]bool)

	for _, spec := range patchSpecs() {
		if spec.when(model) {
			dirs[spec.subdir] = true
		}
	}

	return dirs
}

// generateConditionalPatches generates optional patches from the single
// patchSpecs table. Specs run in declaration order and the first error aborts;
// each enabled spec's content is written via the shared writePatchFile helper
// (empty content and already-existing files without --force are skipped).
func (g *Generator) generateConditionalPatches(
	rootPath string,
	model *Config,
	force bool,
) error {
	for _, spec := range patchSpecs() {
		if !spec.when(model) {
			continue
		}

		// writePatchFile checks exists/!force BEFORE invoking spec.content, so a
		// side-effecting content func (e.g. OIDC reads the CA file) is skipped for
		// an already-present patch — keeping re-generation idempotent.
		err := writePatchFile(rootPath, spec, model, force)
		if err != nil {
			return err
		}
	}

	return nil
}

// createSubdirectories creates the Talos patches subdirectories.
// Only creates .gitkeep files in directories that won't have patches generated.
func (g *Generator) createSubdirectories(
	rootPath string,
	dirsWithPatches map[string]bool,
	force bool,
) error {
	subdirs := []string{
		subdirCluster,
		subdirControlPlanes,
		subdirWorkers,
	}

	for _, subdir := range subdirs {
		dirPath := filepath.Join(rootPath, subdir)

		err := os.MkdirAll(dirPath, dirPerm)
		if err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dirPath, err)
		}

		// Only create .gitkeep if no patches will be generated in this directory
		if dirsWithPatches[subdir] {
			continue
		}

		gitkeepPath := filepath.Join(dirPath, ".gitkeep")

		// Check if .gitkeep already exists
		_, statErr := os.Stat(gitkeepPath)
		if statErr == nil && !force {
			continue
		}

		err = os.WriteFile(gitkeepPath, []byte{}, filePerm)
		if err != nil {
			return fmt.Errorf("failed to create .gitkeep in %s: %w", dirPath, err)
		}
	}

	return nil
}

// generateMirrorPatchYAML generates Talos machine config patch YAML for mirror registries.
// The patch includes the mirrors section with HTTP endpoints.
// No TLS config is needed for HTTP endpoints as containerd will use plain HTTP automatically.
func generateMirrorPatchYAML(specs []registry.MirrorSpec) string {
	if len(specs) == 0 {
		return ""
	}

	var result strings.Builder

	result.WriteString("machine:\n")
	result.WriteString("  registries:\n")
	result.WriteString("    mirrors:\n")

	for _, spec := range specs {
		if spec.Host == "" {
			continue
		}

		result.WriteString("      ")
		result.WriteString(spec.Host)
		result.WriteString(":\n")
		result.WriteString("        endpoints:\n")
		result.WriteString("          - http://")
		result.WriteString(spec.Host)
		result.WriteString(":5000\n")
	}

	// NOTE: We intentionally do NOT add a config section with TLS settings for HTTP endpoints.
	// containerd will reject TLS configuration for non-HTTPS registries with the error:
	// "TLS config specified for non-HTTPS registry"
	// HTTP endpoints work without any additional configuration.

	return result.String()
}

// KubeletCSRApproverInlineManifestPatchYAML returns the Talos machine config patch YAML
// that installs the kubelet-serving-cert-approver via cluster.inlineManifests.
// The manifest uses the upstream-recommended :main image tag.
func KubeletCSRApproverInlineManifestPatchYAML() string {
	manifest := csrapprover.Manifest()
	// Indent manifest content for YAML embedding under contents: |
	indented := indentManifest(manifest, "        ")

	return `cluster:
  inlineManifests:
    - name: kubelet-serving-cert-approver
      contents: |
` + indented + "\n"
}

// indentManifest prepends the given indent to each line of the manifest.
func indentManifest(manifest, indent string) string {
	lines := strings.Split(manifest, "\n")
	indented := make([]string, 0, len(lines))

	for _, line := range lines {
		indented = append(indented, indent+line)
	}

	return strings.Join(indented, "\n")
}

// IngressFirewallDefaultActionYAML is the Talos NetworkDefaultActionConfig document
// that blocks all ingress traffic by default. Individual NetworkRuleConfig documents
// selectively allow required ports.
const IngressFirewallDefaultActionYAML = `apiVersion: v1alpha1
kind: NetworkDefaultActionConfig
ingress: block
`

// networkRuleConfigTemplate is a single Talos NetworkRuleConfig YAML document. %[1]s is the rule
// name, %[2]s the port (or port range/list, pre-formatted), %[3]s the protocol, and %[4]s the
// already-indented `ingress:` list body (e.g. "  - subnet: 10.0.0.0/16\n", or multiple such lines
// for an allowed-CIDR list) — the one building block both the control-plane and worker ingress
// firewall rule sets below are assembled from, instead of each hand-duplicating every document.
const networkRuleConfigTemplate = `apiVersion: v1alpha1
kind: NetworkRuleConfig
name: %[1]s
portSelector:
  ports:
    - %[2]s
  protocol: %[3]s
ingress:
%[4]s`

// networkRuleConfigDoc renders one NetworkRuleConfig document via networkRuleConfigTemplate.
func networkRuleConfigDoc(name, port, protocol, ingressLines string) string {
	return fmt.Sprintf(networkRuleConfigTemplate, name, port, protocol, ingressLines)
}

// singleSubnetIngress formats the common single-CIDR `ingress:` body used by every rule that is
// always restricted to the cluster's own network CIDR (kubelet, trustd, etcd, cni-vxlan) —
// formatIngressSubnets handles the API-facing rules (apid, kubernetes-api), which additionally
// support an allowed-CIDR override.
func singleSubnetIngress(cidr string) string {
	return "  - subnet: " + cidr + "\n"
}

// IngressFirewallCPRulesYAML returns the Talos NetworkRuleConfig documents for control-plane
// nodes. The networkCIDR and cniPort parameters are injected at generation time.
// When allowedCIDRs is non-empty, the apid and kubernetes-api rules use those CIDRs
// instead of 0.0.0.0/0 and ::/0.
//
// This is the single source of truth for the CP rules content, shared between the
// generator (file-based scaffolding) and the runtime config manager (in-memory injection).
func IngressFirewallCPRulesYAML(networkCIDR string, cniPort int, allowedCIDRs []string) string {
	apiIngress := formatIngressSubnets(allowedCIDRs)
	networkIngress := singleSubnetIngress(networkCIDR)

	docs := []string{
		networkRuleConfigDoc("kubelet", "10250", "tcp", networkIngress),
		networkRuleConfigDoc("apid", "50000", "tcp", apiIngress),
		networkRuleConfigDoc("kubernetes-api", "6443", "tcp", apiIngress),
		networkRuleConfigDoc("trustd", "50001", "tcp", networkIngress),
		networkRuleConfigDoc("etcd", "2379-2380", "tcp", networkIngress),
		networkRuleConfigDoc("cni-vxlan", strconv.Itoa(cniPort), "udp", networkIngress),
	}

	return strings.Join(docs, "---\n")
}

// IngressFirewallWorkerRulesYAML returns the Talos NetworkRuleConfig documents for worker
// nodes. Workers expose fewer ports than control-plane nodes.
func IngressFirewallWorkerRulesYAML(networkCIDR string, cniPort int) string {
	networkIngress := singleSubnetIngress(networkCIDR)

	docs := []string{
		networkRuleConfigDoc("kubelet", "10250", "tcp", networkIngress),
		networkRuleConfigDoc("apid", "50000", "tcp", networkIngress),
		networkRuleConfigDoc("cni-vxlan", strconv.Itoa(cniPort), "udp", networkIngress),
	}

	return strings.Join(docs, "---\n")
}

// formatIngressSubnets formats allowed CIDRs (or default 0.0.0.0/0 + ::/0) as
// YAML ingress subnet lines suitable for inclusion in NetworkRuleConfig templates.
// Each line is indented with two spaces (matching the ingress list level).
func formatIngressSubnets(allowedCIDRs []string) string {
	if len(allowedCIDRs) == 0 {
		return "  - subnet: 0.0.0.0/0\n  - subnet: ::/0\n"
	}

	var builder strings.Builder

	for _, cidr := range allowedCIDRs {
		_, ipNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			// Already validated upstream; emit raw value as fallback.
			_, _ = fmt.Fprintf(&builder, "  - subnet: %s\n", strings.TrimSpace(cidr))

			continue
		}

		_, _ = fmt.Fprintf(&builder, "  - subnet: %s\n", ipNet.String())
	}

	return builder.String()
}

var (
	errMissingNetworkCIDR = errors.New("networkCIDR is required when ingress firewall is enabled")
	errInvalidNetworkCIDR = errors.New("networkCIDR is not a valid CIDR")
	errMissingCNIPort     = errors.New("cniPort is required when ingress firewall is enabled")
	errInvalidCNIPort     = errors.New("cniPort must be between 1 and 65535")
)

// validateIngressFirewallModel returns an error if the config fields required by
// ingress firewall patch generation are missing or invalid.
func validateIngressFirewallModel(model *Config) error {
	cidr := strings.TrimSpace(model.NetworkCIDR)
	if cidr == "" {
		return errMissingNetworkCIDR
	}

	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidNetworkCIDR, err)
	}

	model.NetworkCIDR = ipNet.String()

	if model.CNIPort == 0 {
		return errMissingCNIPort
	}

	if model.CNIPort < 1 || model.CNIPort > 65535 {
		return fmt.Errorf("%w: got %d", errInvalidCNIPort, model.CNIPort)
	}

	return nil
}

func writeOIDCAPIServerArgs(builder *strings.Builder, model *Config) {
	_, _ = fmt.Fprintf(builder, "cluster:\n")
	_, _ = fmt.Fprintf(builder, "  apiServer:\n")
	_, _ = fmt.Fprintf(builder, "    extraArgs:\n")
	_, _ = fmt.Fprintf(builder, "      oidc-issuer-url: %q\n", model.OIDCIssuerURL)
	_, _ = fmt.Fprintf(builder, "      oidc-client-id: %q\n", model.OIDCClientID)

	if model.OIDCUsernameClaim != "" {
		_, _ = fmt.Fprintf(builder, "      oidc-username-claim: %q\n", model.OIDCUsernameClaim)
	}

	if model.OIDCUsernamePrefix != "" {
		_, _ = fmt.Fprintf(builder, "      oidc-username-prefix: %q\n", model.OIDCUsernamePrefix)
	}

	if model.OIDCGroupsClaim != "" {
		_, _ = fmt.Fprintf(builder, "      oidc-groups-claim: %q\n", model.OIDCGroupsClaim)
	}

	if model.OIDCGroupsPrefix != "" {
		_, _ = fmt.Fprintf(builder, "      oidc-groups-prefix: %q\n", model.OIDCGroupsPrefix)
	}

	if model.OIDCCAFile != "" {
		_, _ = fmt.Fprintf(builder, "      oidc-ca-file: %q\n", v1alpha1.OIDCCAContainerPath)
	}
}

// writeOIDCCAMachineFiles embeds the OIDC CA certificate content into a Talos
// machine.files block, making it available at OIDCCAContainerPath on the node.
func writeOIDCCAMachineFiles(builder *strings.Builder, caFilePath string) error {
	caContent, err := readOIDCCAFile(caFilePath)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(builder, "machine:\n")
	_, _ = fmt.Fprintf(builder, "  files:\n")
	_, _ = fmt.Fprintf(builder, "    - content: |\n")

	for line := range strings.SplitSeq(strings.TrimRight(string(caContent), "\n"), "\n") {
		_, _ = fmt.Fprintf(builder, "        %s\n", line)
	}

	_, _ = fmt.Fprintf(builder, "      permissions: 0o644\n")
	_, _ = fmt.Fprintf(builder, "      path: %s\n", v1alpha1.OIDCCAContainerPath)
	_, _ = fmt.Fprintf(builder, "      op: create\n")
	_, _ = fmt.Fprintf(builder, "---\n")

	return nil
}

func readOIDCCAFile(caFilePath string) ([]byte, error) {
	canonicalCAPath, err := fsutil.EvalCanonicalPath(caFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve OIDC CA file path %q: %w", caFilePath, err)
	}

	caContent, err := os.ReadFile(canonicalCAPath) //nolint:gosec // path canonicalized above
	if err != nil {
		return nil, fmt.Errorf("failed to read OIDC CA file %q: %w", canonicalCAPath, err)
	}

	return caContent, nil
}

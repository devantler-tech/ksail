package talosgenerator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
)

// KubeletServingCertApproverManifestURL is the URL for the kubelet-serving-cert-approver manifest.
// This is installed during Talos bootstrap to automatically approve kubelet serving certificate CSRs.
// Note: We use alex1989hu/kubelet-serving-cert-approver for Talos because it provides a single
// manifest URL suitable for extraManifests. For non-Talos distributions, we use
// postfinance/kubelet-csr-approver via Helm which offers more features and configurability.
// See: https://docs.siderolabs.com/kubernetes-guides/monitoring-and-observability/deploy-metrics-server/
//
//nolint:lll // URL cannot be shortened
const KubeletServingCertApproverManifestURL = "https://raw.githubusercontent.com/alex1989hu/kubelet-serving-cert-approver/main/deploy/standalone-install.yaml"

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
	// When true, generates a disable-default-cni.yaml patch to set cluster.network.cni.name to "none".
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

// getDirectoriesWithPatches returns a set of subdirectory names that will have patches generated.
func (g *Generator) getDirectoriesWithPatches(
	model *Config,
) map[string]bool {
	dirs := make(map[string]bool)

	// Mirror registries patch goes to cluster/
	if len(model.MirrorRegistries) > 0 {
		dirs["cluster"] = true
	}

	// Allow scheduling patch goes to cluster/
	if model.WorkerNodes == 0 {
		dirs["cluster"] = true
	}

	// Disable CNI patch goes to cluster/
	if model.DisableDefaultCNI {
		dirs["cluster"] = true
	}

	// Kubelet cert rotation patch goes to cluster/
	if model.EnableKubeletCertRotation {
		dirs["cluster"] = true
	}

	// Cluster name patch goes to cluster/
	if model.ClusterName != "" {
		dirs["cluster"] = true
	}

	// Image verification config document goes to cluster/ —
	// Talos configpatcher.LoadPatch correctly recognizes it as an ImageVerificationConfig
	// document and StrategicMerge appends it to the config bundle (it does NOT overwrite
	// the MachineConfig since it has a different kind).
	if model.EnableImageVerification {
		dirs["cluster"] = true
	}

	// Disable CDI patch goes to cluster/
	if model.DisableCDI {
		dirs["cluster"] = true
	}

	// External cloud provider patch goes to cluster/
	if model.EnableExternalCloudProvider {
		dirs["cluster"] = true
	}

	return dirs
}

// generateConditionalPatches generates optional patches based on the configuration.
//
//nolint:cyclop,funlen // Sequential conditional patch generation - each condition is independent and simple.
func (g *Generator) generateConditionalPatches(
	rootPath string,
	model *Config,
	force bool,
) error {
	// Generate mirror registries patch if configured
	if len(model.MirrorRegistries) > 0 {
		err := g.generateMirrorRegistriesPatch(rootPath, model.MirrorRegistries, force)
		if err != nil {
			return err
		}
	}

	// Generate allow-scheduling-on-control-planes patch when no workers are configured
	if model.WorkerNodes == 0 {
		err := g.generateAllowSchedulingPatch(rootPath, force)
		if err != nil {
			return err
		}
	}

	// Generate disable-default-cni patch when alternative CNI is requested
	if model.DisableDefaultCNI {
		err := g.generateDisableCNIPatch(rootPath, force)
		if err != nil {
			return err
		}
	}

	// Generate kubelet-cert-rotation patch for secure metrics-server TLS.
	// The kubelet-csr-approver is installed via extraManifests during bootstrap.
	if model.EnableKubeletCertRotation {
		err := g.generateKubeletCertRotationPatch(rootPath, force)
		if err != nil {
			return err
		}

		// Generate kubelet-csr-approver patch to install the CSR approver during bootstrap
		err = g.generateKubeletCSRApproverPatch(rootPath, force)
		if err != nil {
			return err
		}
	}

	// Generate cluster-name patch when custom cluster name is specified
	if model.ClusterName != "" {
		err := g.generateClusterNamePatch(rootPath, model.ClusterName, force)
		if err != nil {
			return err
		}
	}

	// Generate image verification config document when enabled
	if model.EnableImageVerification {
		err := g.generateImageVerificationPatch(rootPath, force)
		if err != nil {
			return err
		}
	}

	// Generate disable-cdi patch when CDI should be turned off
	if model.DisableCDI {
		err := g.generateDisableCDIPatch(rootPath, force)
		if err != nil {
			return err
		}
	}

	// Generate external cloud provider patch when cloud provider integration is needed
	if model.EnableExternalCloudProvider {
		err := g.generateExternalCloudProviderPatch(rootPath, force)
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
		"cluster",
		"control-planes",
		"workers",
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

// generateMirrorRegistriesPatch creates a Talos patch file for registry mirrors.
func (g *Generator) generateMirrorRegistriesPatch(
	rootPath string,
	mirrorRegistries []string,
	force bool,
) error {
	// Parse mirror specs
	specs := registry.ParseMirrorSpecs(mirrorRegistries)
	if len(specs) == 0 {
		return nil
	}

	// Generate YAML content
	patchContent := generateMirrorPatchYAML(specs)
	if patchContent == "" {
		return nil
	}

	// Write to cluster patches directory
	patchPath := filepath.Join(rootPath, "cluster", mirrorRegistriesFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create mirror registries patch: %w", err)
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

// generateAllowSchedulingPatch creates a Talos patch file to allow scheduling on control-plane nodes.
// This is required for single-node clusters or clusters with only control-plane nodes.
func (g *Generator) generateAllowSchedulingPatch(
	rootPath string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", allowSchedulingFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	patchContent := `cluster:
  allowSchedulingOnControlPlanes: true
`

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create allow-scheduling-on-control-planes patch: %w", err)
	}

	return nil
}

// generateDisableCNIPatch creates a Talos patch file to disable the default CNI (Flannel).
// This is required when using an alternative CNI like Cilium.
// The patch sets cluster.network.cni.name to "none" as per Talos documentation:
// https://docs.siderolabs.com/kubernetes-guides/cni/deploying-cilium
func (g *Generator) generateDisableCNIPatch(
	rootPath string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", disableCNIFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	patchContent := `cluster:
  network:
    cni:
      name: none
`

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create disable-default-cni patch: %w", err)
	}

	return nil
}

// generateKubeletCertRotationPatch creates a Talos patch file to enable kubelet serving certificate rotation.
// This is required for secure metrics-server communication using TLS.
// The patch sets machine.kubelet.extraArgs.rotate-server-certificates to "true" as per Talos documentation:
// https://www.talos.dev/v1.9/kubernetes-guides/configuration/deploy-metrics-server/
func (g *Generator) generateKubeletCertRotationPatch(
	rootPath string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", kubeletCertRotationFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	patchContent := `machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: "true"
`

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create kubelet-cert-rotation patch: %w", err)
	}

	return nil
}

// generateKubeletCSRApproverPatch creates a Talos patch file to install the kubelet-serving-cert-approver
// during cluster bootstrap. This is required when rotate-server-certificates is enabled because:
// 1. The kubelet generates a CSR (Certificate Signing Request) for its serving certificate
// 2. The CSR must be approved before the kubelet can serve its API (including to metrics-server)
// 3. Without an approver, the cluster bootstrap times out waiting for static pods
//
// This patch adds cluster.extraManifests with the kubelet-serving-cert-approver manifest URL.
// See: https://docs.siderolabs.com/kubernetes-guides/monitoring-and-observability/deploy-metrics-server/
func (g *Generator) generateKubeletCSRApproverPatch(
	rootPath string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", kubeletCSRApproverFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	patchContent := `cluster:
  extraManifests:
    - ` + KubeletServingCertApproverManifestURL + `
`

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create kubelet-csr-approver patch: %w", err)
	}

	return nil
}

// generateClusterNamePatch creates a Talos patch file to set a custom cluster name.
// The cluster name is used in the kubeconfig context (admin@<name>) and for
// container naming conventions.
// Note: The cluster name is expected to be pre-validated as DNS-1123 compliant
// (lowercase alphanumeric and hyphens only), which makes direct template
// construction safe from injection attacks.
func (g *Generator) generateClusterNamePatch(
	rootPath string,
	clusterName string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", clusterNameFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	// Use simple template format matching other Talos patches.
	// The cluster name is validated as DNS-1123 before reaching this point,
	// so it contains only [a-z0-9-] characters, making this safe.
	patchContent := fmt.Sprintf(`cluster:
  clusterName: %s
`, clusterName)

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create cluster-name patch: %w", err)
	}

	return nil
}

// generateImageVerificationPatch creates a Talos ImageVerificationConfig document.
// The document is placed in cluster/ alongside other patches. Talos configpatcher recognizes
// it as a registered config document (kind: ImageVerificationConfig) and StrategicMerge
// appends it to the config bundle — it does NOT overwrite the MachineConfig.
// See: https://docs.siderolabs.com/talos/v1.13/security/verifying-image-signatures
func (g *Generator) generateImageVerificationPatch(
	rootPath string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", imageVerificationFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	patchContent := `# Talos ImageVerificationConfig (Talos 1.13+)
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

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create image-verification config: %w", err)
	}

	return nil
}

// generateDisableCDIPatch creates a Talos patch file to disable CDI.
// Talos 1.13+ enables CDI (Container Device Interface) by default via machine.features.
// This patch explicitly disables CDI when the user sets CDI to Disabled.
func (g *Generator) generateDisableCDIPatch(
	rootPath string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", disableCDIFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	patchContent := `machine:
  features:
    enableCDI: false
`

	err := os.WriteFile(patchPath, []byte(patchContent), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create disable-cdi patch: %w", err)
	}

	return nil
}

// generateExternalCloudProviderPatch creates a Talos patch file to enable the external
// cloud provider. This is required for cloud providers like Hetzner Cloud so that the
// Cloud Controller Manager (CCM) can:
//  1. Initialize nodes with a providerID (spec.providerID)
//  2. Write node labels that the CSI DaemonSet requires for scheduling
//
// Without this patch, nodes come up Ready but without providerID, and CSI pods never
// schedule because their node affinity depends on labels written by CCM.
//
// See: https://www.talos.dev/latest/kubernetes-guides/configuration/cloud-provider/
func (g *Generator) generateExternalCloudProviderPatch(
	rootPath string,
	force bool,
) error {
	patchPath := filepath.Join(rootPath, "cluster", externalCloudProviderFileName)

	// Check if file already exists
	_, statErr := os.Stat(patchPath)
	if statErr == nil && !force {
		return nil
	}

	err := os.WriteFile(patchPath, []byte(ExternalCloudProviderPatchYAML), filePerm)
	if err != nil {
		return fmt.Errorf("failed to create external-cloud-provider patch: %w", err)
	}

	return nil
}

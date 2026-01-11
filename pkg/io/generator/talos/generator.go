package talosgenerator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
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

// ErrConfigRequired is returned when a nil config is provided.
var ErrConfigRequired = errors.New("talos config is required")

// TalosConfig represents the Talos scaffolding configuration.
type TalosConfig struct {
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
}

// TalosGenerator generates the Talos directory structure.
type TalosGenerator struct{}

// NewTalosGenerator creates a new TalosGenerator.
func NewTalosGenerator() *TalosGenerator {
	return &TalosGenerator{}
}

// Generate creates the Talos patches directory structure.
// The model parameter contains the patches directory path.
// Returns the generated directory path and any error encountered.
func (g *TalosGenerator) Generate(
	model *TalosConfig,
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
func (g *TalosGenerator) getDirectoriesWithPatches(
	model *TalosConfig,
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

	return dirs
}

// generateConditionalPatches generates optional patches based on the configuration.
func (g *TalosGenerator) generateConditionalPatches(
	rootPath string,
	model *TalosConfig,
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

	return nil
}

// createSubdirectories creates the Talos patches subdirectories.
// Only creates .gitkeep files in directories that won't have patches generated.
func (g *TalosGenerator) createSubdirectories(
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
func (g *TalosGenerator) generateMirrorRegistriesPatch(
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
func (g *TalosGenerator) generateAllowSchedulingPatch(
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
func (g *TalosGenerator) generateDisableCNIPatch(
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
func (g *TalosGenerator) generateKubeletCertRotationPatch(
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
func (g *TalosGenerator) generateKubeletCSRApproverPatch(
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
func (g *TalosGenerator) generateClusterNamePatch(
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

package talosgenerator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	registryutil "github.com/devantler-tech/ksail/v5/pkg/registry"
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
)

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
	specs := registryutil.ParseMirrorSpecs(mirrorRegistries)
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
func generateMirrorPatchYAML(specs []registryutil.MirrorSpec) string {
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

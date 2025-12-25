package talosgenerator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
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
)

// ErrConfigRequired is returned when a nil config is provided.
var ErrConfigRequired = errors.New("talosindocker config is required")

// TalosInDockerConfig represents the TalosInDocker scaffolding configuration.
type TalosInDockerConfig struct {
	// PatchesDir is the root directory for Talos patches.
	PatchesDir string
	// MirrorRegistries contains mirror registry specifications in "host=upstream" format.
	// Example: ["docker.io=https://registry-1.docker.io"]
	MirrorRegistries []string
	// WorkerNodes is the number of worker nodes configured.
	// When 0 (default), generates allow-scheduling-on-control-planes.yaml.
	WorkerNodes int
}

// TalosInDockerGenerator generates the TalosInDocker directory structure.
type TalosInDockerGenerator struct{}

// NewTalosInDockerGenerator creates a new TalosInDockerGenerator.
func NewTalosInDockerGenerator() *TalosInDockerGenerator {
	return &TalosInDockerGenerator{}
}

// Generate creates the TalosInDocker patches directory structure.
// The model parameter contains the patches directory path.
// Returns the generated directory path and any error encountered.
func (g *TalosInDockerGenerator) Generate(
	model *TalosInDockerConfig,
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

	// Create subdirectories with .gitkeep files
	err := g.createSubdirectories(rootPath, opts.Force)
	if err != nil {
		return "", err
	}

	// Generate mirror registries patch if configured
	if len(model.MirrorRegistries) > 0 {
		err := g.generateMirrorRegistriesPatch(rootPath, model.MirrorRegistries, opts.Force)
		if err != nil {
			return "", err
		}
	}

	// Generate allow-scheduling-on-control-planes patch when no workers are configured
	if model.WorkerNodes == 0 {
		err := g.generateAllowSchedulingPatch(rootPath, opts.Force)
		if err != nil {
			return "", err
		}
	}

	return rootPath, nil
}

// createSubdirectories creates the Talos patches subdirectories with .gitkeep files.
func (g *TalosInDockerGenerator) createSubdirectories(rootPath string, force bool) error {
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
func (g *TalosInDockerGenerator) generateMirrorRegistriesPatch(
	rootPath string,
	mirrorRegistries []string,
	force bool,
) error {
	// Parse mirror specs
	specs := registry.ParseMirrorSpecs(mirrorRegistries)
	if len(specs) == 0 {
		return nil
	}

	// Generate YAML content using shared implementation
	patchContent := talosindockerprovisioner.GenerateMirrorPatchYAML(specs)
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

// generateAllowSchedulingPatch creates a Talos patch file to allow scheduling on control-plane nodes.
// This is required for single-node clusters or clusters with only control-plane nodes.
func (g *TalosInDockerGenerator) generateAllowSchedulingPatch(
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

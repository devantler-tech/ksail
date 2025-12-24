package talosgenerator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// generateMirrorPatchYAML generates a Talos machine config patch for registry mirrors.
func generateMirrorPatchYAML(specs []registry.MirrorSpec) string {
	if len(specs) == 0 {
		return ""
	}

	// Sort specs by host for deterministic output
	sortedSpecs := make([]registry.MirrorSpec, len(specs))
	copy(sortedSpecs, specs)
	sort.Slice(sortedSpecs, func(i, j int) bool {
		return sortedSpecs[i].Host < sortedSpecs[j].Host
	})

	var builder strings.Builder
	builder.WriteString("machine:\n")
	builder.WriteString("  registries:\n")
	builder.WriteString("    mirrors:\n")

	for _, spec := range sortedSpecs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		// Determine the upstream URL
		upstream := strings.TrimSpace(spec.Remote)
		if upstream == "" {
			upstream = registry.GenerateUpstreamURL(host)
		}

		// Generate the mirror entry
		builder.WriteString(fmt.Sprintf("      %s:\n", host))
		builder.WriteString("        endpoints:\n")
		builder.WriteString(fmt.Sprintf("          - %s\n", upstream))

		// Add the default upstream as fallback
		defaultUpstream := registry.GenerateUpstreamURL(host)
		if upstream != defaultUpstream {
			builder.WriteString(fmt.Sprintf("          - %s\n", defaultUpstream))
		}

		builder.WriteString("        overridePath: true\n")
	}

	return builder.String()
}

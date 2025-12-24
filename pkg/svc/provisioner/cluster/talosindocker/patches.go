package talosindockerprovisioner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrConfigNil is returned when a nil configuration is provided.
var ErrConfigNil = errors.New("configuration cannot be nil")

// PatchScope indicates which nodes a Talos patch applies to.
type PatchScope string

const (
	// PatchScopeCluster applies the patch to all nodes.
	PatchScopeCluster PatchScope = "cluster"
	// PatchScopeControlPlane applies the patch to control-plane nodes only.
	PatchScopeControlPlane PatchScope = "control-plane"
	// PatchScopeWorker applies the patch to worker nodes only.
	PatchScopeWorker PatchScope = "worker"
)

// TalosPatch represents a Talos machine configuration patch.
type TalosPatch struct {
	// Path is the absolute path to the patch file.
	Path string

	// Scope indicates which nodes this patch applies to.
	Scope PatchScope

	// Content is the raw YAML content of the patch.
	Content []byte
}

// LoadPatches loads all Talos patches from the configured directories.
// It returns patches organized by scope for application during cluster creation.
func LoadPatches(config *TalosInDockerConfig) ([]TalosPatch, error) {
	if config == nil {
		return nil, ErrConfigNil
	}

	var patches []TalosPatch

	// Load patches from each directory with appropriate scope
	clusterPatches, err := loadPatchesFromDir(config.ClusterPatchesDir, PatchScopeCluster)
	if err != nil {
		return nil, err
	}

	patches = append(patches, clusterPatches...)

	controlPlanePatches, err := loadPatchesFromDir(
		config.ControlPlanePatchesDir,
		PatchScopeControlPlane,
	)
	if err != nil {
		return nil, err
	}

	patches = append(patches, controlPlanePatches...)

	workerPatches, err := loadPatchesFromDir(config.WorkerPatchesDir, PatchScopeWorker)
	if err != nil {
		return nil, err
	}

	patches = append(patches, workerPatches...)

	return patches, nil
}

// loadPatchesFromDir reads all YAML files from a directory and returns them as patches.
func loadPatchesFromDir(dir string, scope PatchScope) ([]TalosPatch, error) {
	// Check if directory exists
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		// Directory doesn't exist - not an error, just no patches
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to stat directory %s: %w", dir, err)
	}

	if !info.IsDir() {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	patches := make([]TalosPatch, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !isYAMLFile(name) {
			continue
		}

		filePath := filepath.Join(dir, name)

		//nolint:gosec // G304: File path is constructed from config, not user input
		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read patch file %s: %w", filePath, err)
		}

		// Get absolute path for consistent patch references
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for %s: %w", filePath, err)
		}

		patches = append(patches, TalosPatch{
			Path:    absPath,
			Scope:   scope,
			Content: content,
		})
	}

	return patches, nil
}

// isYAMLFile returns true if the filename has a YAML extension.
func isYAMLFile(name string) bool {
	lower := strings.ToLower(name)

	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

package tenant

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"sigs.k8s.io/yaml"
)

const kustomizationFilePermissions = 0o600

// RegisterTenant adds a tenant's directory path to a kustomization.yaml resources list.
// If kustomizationPath is empty, auto-discovers by walking up from outputDir.
func RegisterTenant(tenantName, outputDir, kustomizationPath string) error {
	return modifyKustomizationResources(tenantName, outputDir, kustomizationPath, addResource)
}

// UnregisterTenant removes a tenant's directory path from a kustomization.yaml resources list.
func UnregisterTenant(tenantName, outputDir, kustomizationPath string) error {
	return modifyKustomizationResources(tenantName, outputDir, kustomizationPath, removeResource)
}

// modifyKustomizationResources resolves the kustomization path, reads its contents,
// applies a resource transformation, and writes it back.
func modifyKustomizationResources(
	tenantName, outputDir, kustomizationPath string,
	transform func(resources []string, relPath string) []string,
) error {
	kPath, err := resolveKustomizationPath(outputDir, kustomizationPath)
	if err != nil {
		return err
	}

	raw, err := readKustomizationRaw(kPath)
	if err != nil {
		return err
	}

	relPath, err := computeRelativePath(kPath, outputDir, tenantName)
	if err != nil {
		return err
	}

	raw["resources"] = transform(getResources(raw), relPath)
	return writeKustomizationRaw(kPath, raw)
}

func addResource(resources []string, relPath string) []string {
	for _, r := range resources {
		if r == relPath {
			return resources // already registered
		}
	}
	return append(resources, relPath)
}

func removeResource(resources []string, relPath string) []string {
	filtered := make([]string, 0, len(resources))
	for _, r := range resources {
		if r != relPath {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FindKustomization walks up from startDir looking for kustomization.yaml.
// Returns the path to the first kustomization.yaml found, or error if none found.
func FindKustomization(startDir string) (string, error) {
	dir, err := fsutil.EvalCanonicalPath(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve start dir: %w", err)
	}

	for {
		candidate := filepath.Join(dir, "kustomization.yaml")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrKustomizationNotFound
		}
		dir = parent
	}
}

func resolveKustomizationPath(outputDir, explicit string) (string, error) {
	if explicit != "" {
		canonical, err := fsutil.EvalCanonicalPath(explicit)
		if err != nil {
			return "", fmt.Errorf("resolving kustomization path: %w", err)
		}
		if _, err := os.Stat(canonical); err != nil {
			return "", fmt.Errorf("kustomization file not found: %w", err)
		}
		return canonical, nil
	}
	return FindKustomization(outputDir)
}

// readKustomizationRaw reads a kustomization.yaml into a raw map,
// preserving all fields (patches, images, namespace, generators, etc.).
func readKustomizationRaw(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is already canonicalized
	if err != nil {
		return nil, fmt.Errorf("read kustomization: %w", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal kustomization: %w", err)
	}
	if raw == nil {
		raw = make(map[string]any)
	}
	return raw, nil
}

// writeKustomizationRaw writes a raw map back to kustomization.yaml.
func writeKustomizationRaw(path string, raw map[string]any) error {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal kustomization: %w", err)
	}
	if err := os.WriteFile(path, data, kustomizationFilePermissions); err != nil {
		return fmt.Errorf("write kustomization: %w", err)
	}
	return nil
}

// getResources extracts the resources list from a raw kustomization map.
func getResources(raw map[string]any) []string {
	res, ok := raw["resources"]
	if !ok {
		return nil
	}
	slice, ok := res.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func computeRelativePath(kustomizationPath, outputDir, tenantName string) (string, error) {
	kDir := filepath.Dir(kustomizationPath)

	absKDir, err := fsutil.EvalCanonicalPath(kDir)
	if err != nil {
		return "", fmt.Errorf("resolve kustomization dir: %w", err)
	}
	absOutputDir, err := fsutil.EvalCanonicalPath(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}

	tenantDir := filepath.Join(absOutputDir, tenantName)

	rel, err := filepath.Rel(absKDir, tenantDir)
	if err != nil {
		return "", fmt.Errorf("compute relative path: %w", err)
	}

	// Reject paths that escape the kustomization root.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q is outside %q", ErrOutsideKustomizationRoot, tenantDir, absKDir)
	}

	// Normalize to forward slashes for YAML compatibility.
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
	return rel, nil
}

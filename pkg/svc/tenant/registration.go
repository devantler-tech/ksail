package tenant

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"k8s.io/apimachinery/pkg/util/validation"
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
	if errs := validation.IsDNS1123Label(tenantName); len(errs) > 0 {
		return fmt.Errorf(
			"%w: %s (%s)",
			ErrInvalidTenantName, tenantName, strings.Join(errs, "; "),
		)
	}

	kPath, err := ResolveKustomizationPath(outputDir, kustomizationPath)
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
	if slices.Contains(resources, relPath) {
		return resources // already registered
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

		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrKustomizationNotFound
		}

		dir = parent
	}
}

// ResolveKustomizationPath resolves the kustomization.yaml path from an explicit
// path or by auto-discovering it by walking up from outputDir.
func ResolveKustomizationPath(outputDir, explicit string) (string, error) {
	if explicit != "" {
		canonical, err := fsutil.EvalCanonicalPath(explicit)
		if err != nil {
			return "", fmt.Errorf("resolving kustomization path: %w", err)
		}

		info, statErr := os.Stat(canonical)
		if statErr != nil {
			return "", fmt.Errorf("kustomization file not found: %w", statErr)
		}

		if info.IsDir() {
			return "", fmt.Errorf(
				"%w: %q", ErrKustomizationIsDirectory, canonical,
			)
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

	unmarshalErr := yaml.Unmarshal(data, &raw)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("unmarshal kustomization: %w", unmarshalErr)
	}

	if raw == nil {
		raw = make(map[string]any)
	}

	return raw, nil
}

// writeKustomizationRaw writes a raw map back to kustomization.yaml,
// preserving the original file permissions when the file already exists.
func writeKustomizationRaw(path string, raw map[string]any) error {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal kustomization: %w", err)
	}

	perm := os.FileMode(kustomizationFilePermissions)

	info, statErr := os.Stat(path)
	if statErr == nil {
		perm = info.Mode().Perm()
	}

	writeErr := os.WriteFile(path, data, perm)
	if writeErr != nil {
		return fmt.Errorf("write kustomization: %w", writeErr)
	}

	return nil
}

// getResources extracts the resources list from a raw kustomization map.
func getResources(raw map[string]any) []string {
	res, exists := raw["resources"]
	if !exists {
		return nil
	}

	slice, isSlice := res.([]any)
	if !isSlice {
		return nil
	}

	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if strVal, isStr := item.(string); isStr {
			result = append(result, strVal)
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
		return "", fmt.Errorf(
			"%w: %q is outside %q",
			ErrOutsideKustomizationRoot,
			tenantDir,
			absKDir,
		)
	}

	// Normalize to forward slashes for YAML compatibility.
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")

	return rel, nil
}

// RegisterResource adds a resource name to a kustomization.yaml's resources list.
// Idempotent — does nothing if the resource is already listed.
func RegisterResource(kustomizationPath, resourceName string) error {
	canonPath, err := fsutil.EvalCanonicalPath(kustomizationPath)
	if err != nil {
		return fmt.Errorf("resolving kustomization path: %w", err)
	}

	raw, err := readKustomizationRaw(canonPath)
	if err != nil {
		return err
	}

	resources := getResources(raw)
	if slices.Contains(resources, resourceName) {
		return nil
	}

	raw["resources"] = addResource(resources, resourceName)

	return writeKustomizationRaw(canonPath, raw)
}

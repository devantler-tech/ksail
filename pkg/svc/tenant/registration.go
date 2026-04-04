package tenant

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

// kustomizationType represents a minimal kustomization.yaml structure.
type kustomizationType struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Resources  []string `json:"resources"`
}

// RegisterTenant adds a tenant's directory path to a kustomization.yaml resources list.
// If kustomizationPath is empty, auto-discovers by walking up from outputDir.
func RegisterTenant(tenantName, outputDir, kustomizationPath string) error {
	kPath, err := resolveKustomizationPath(outputDir, kustomizationPath)
	if err != nil {
		return err
	}

	k, err := readKustomization(kPath)
	if err != nil {
		return err
	}

	relPath, err := computeRelativePath(kPath, outputDir, tenantName)
	if err != nil {
		return err
	}

	for _, r := range k.Resources {
		if r == relPath {
			return nil // already registered
		}
	}

	k.Resources = append(k.Resources, relPath)
	return writeKustomization(kPath, k)
}

// UnregisterTenant removes a tenant's directory path from a kustomization.yaml resources list.
func UnregisterTenant(tenantName, outputDir, kustomizationPath string) error {
	kPath, err := resolveKustomizationPath(outputDir, kustomizationPath)
	if err != nil {
		return err
	}

	k, err := readKustomization(kPath)
	if err != nil {
		return err
	}

	relPath, err := computeRelativePath(kPath, outputDir, tenantName)
	if err != nil {
		return err
	}

	filtered := make([]string, 0, len(k.Resources))
	for _, r := range k.Resources {
		if r != relPath {
			filtered = append(filtered, r)
		}
	}
	k.Resources = filtered

	return writeKustomization(kPath, k)
}

// FindKustomization walks up from startDir looking for kustomization.yaml.
// Returns the path to the first kustomization.yaml found, or error if none found.
func FindKustomization(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
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
		return explicit, nil
	}
	return FindKustomization(outputDir)
}

func readKustomization(path string) (*kustomizationType, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read kustomization: %w", err)
	}
	var k kustomizationType
	if err := yaml.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("unmarshal kustomization: %w", err)
	}
	return &k, nil
}

func writeKustomization(path string, k *kustomizationType) error {
	data, err := yaml.Marshal(k)
	if err != nil {
		return fmt.Errorf("marshal kustomization: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write kustomization: %w", err)
	}
	return nil
}

func computeRelativePath(kustomizationPath, outputDir, tenantName string) (string, error) {
	kDir := filepath.Dir(kustomizationPath)

	absKDir, err := filepath.Abs(kDir)
	if err != nil {
		return "", fmt.Errorf("resolve kustomization dir: %w", err)
	}
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}

	tenantDir := filepath.Join(absOutputDir, tenantName)

	rel, err := filepath.Rel(absKDir, tenantDir)
	if err != nil {
		return "", fmt.Errorf("compute relative path: %w", err)
	}

	// Normalize to forward slashes for YAML compatibility.
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
	return rel, nil
}

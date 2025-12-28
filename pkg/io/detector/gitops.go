package detector

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	// ManagedByLabel is the label key used to identify KSail-managed resources.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	// ManagedByValue is the label value for KSail-managed resources.
	ManagedByValue = "ksail"

	// FluxInstanceAPIVersion is the API version for FluxInstance CRs.
	FluxInstanceAPIVersion = "fluxcd.controlplane.io/v1"
	// FluxInstanceKind is the kind for FluxInstance CRs.
	FluxInstanceKind = "FluxInstance"
	// FluxInstanceDefaultName is the default name for KSail-managed FluxInstance.
	FluxInstanceDefaultName = "flux"
	// FluxInstanceNamespace is the namespace for FluxInstance CRs.
	FluxInstanceNamespace = "flux-system"

	// ArgoCDApplicationAPIVersion is the API version for ArgoCD Application CRs.
	ArgoCDApplicationAPIVersion = "argoproj.io/v1alpha1"
	// ArgoCDApplicationKind is the kind for ArgoCD Application CRs.
	ArgoCDApplicationKind = "Application"
	// ArgoCDApplicationDefaultName is the default name for KSail-managed ArgoCD Application.
	ArgoCDApplicationDefaultName = "ksail"
	// ArgoCDNamespace is the namespace for ArgoCD resources.
	ArgoCDNamespace = "argocd"
)

// k8sResource represents the minimal structure needed to identify a Kubernetes resource.
type k8sResource struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       string          `yaml:"kind"`
	Metadata   k8sResourceMeta `yaml:"metadata"`
}

type k8sResourceMeta struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

// GitOpsCRDetector detects existing GitOps Custom Resources in a source directory.
type GitOpsCRDetector struct {
	sourceDir string
}

// NewGitOpsCRDetector creates a new detector for the given source directory.
func NewGitOpsCRDetector(sourceDir string) *GitOpsCRDetector {
	return &GitOpsCRDetector{sourceDir: sourceDir}
}

// FindFluxInstance searches for an existing KSail-managed FluxInstance CR.
// Returns the file path if found, empty string otherwise.
func (d *GitOpsCRDetector) FindFluxInstance() (string, error) {
	return d.findCR(FluxInstanceAPIVersion, FluxInstanceKind, FluxInstanceDefaultName)
}

// FindArgoCDApplication searches for an existing KSail-managed ArgoCD Application CR.
// Returns the file path if found, empty string otherwise.
func (d *GitOpsCRDetector) FindArgoCDApplication() (string, error) {
	return d.findCR(
		ArgoCDApplicationAPIVersion,
		ArgoCDApplicationKind,
		ArgoCDApplicationDefaultName,
	)
}

// findCR searches recursively for a CR matching the given criteria.
func (d *GitOpsCRDetector) findCR(apiVersion, kind, defaultName string) (string, error) {
	_, err := os.Stat(d.sourceDir)
	if os.IsNotExist(err) {
		return "", nil
	}

	var foundPath string

	err = filepath.WalkDir(d.sourceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		if !isYAMLFile(path) {
			return nil
		}

		match, matchErr := d.checkFile(path, apiVersion, kind, defaultName)
		if matchErr != nil {
			// Skip files that can't be parsed
			return nil //nolint:nilerr // intentionally skip unparseable files
		}

		if match {
			foundPath = path

			return filepath.SkipAll
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walking source directory: %w", err)
	}

	return foundPath, nil
}

// checkFile checks if a file contains a matching CR.
func (d *GitOpsCRDetector) checkFile(path, apiVersion, kind, defaultName string) (bool, error) {
	// G304: path is validated via WalkDir starting from trusted sourceDir
	data, err := os.ReadFile(path) //nolint:gosec // path is validated via WalkDir
	if err != nil {
		return false, fmt.Errorf("reading file: %w", err)
	}

	// Handle multi-document YAML files
	docs := splitYAMLDocuments(data)
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}

		var resource k8sResource

		err := yaml.Unmarshal(doc, &resource)
		if err != nil {
			continue
		}

		if d.isMatchingCR(resource, apiVersion, kind, defaultName) {
			return true, nil
		}
	}

	return false, nil
}

// isMatchingCR checks if a resource matches the detection criteria.
func (d *GitOpsCRDetector) isMatchingCR(
	resource k8sResource,
	apiVersion, kind, defaultName string,
) bool {
	// Must match apiVersion and kind
	if resource.APIVersion != apiVersion || resource.Kind != kind {
		return false
	}

	// Match on any of:
	// 1. Name matches the default KSail name
	// 2. Has managed-by: ksail label
	if resource.Metadata.Name == defaultName {
		return true
	}

	if resource.Metadata.Labels != nil {
		if resource.Metadata.Labels[ManagedByLabel] == ManagedByValue {
			return true
		}
	}

	return false
}

// isYAMLFile checks if the file has a YAML extension.
func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	return ext == ".yaml" || ext == ".yml"
}

// splitYAMLDocuments splits a multi-document YAML file into individual documents.
func splitYAMLDocuments(data []byte) [][]byte {
	// Split on YAML document separator
	parts := strings.Split(string(data), "\n---")
	docs := make([][]byte, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			docs = append(docs, []byte(trimmed))
		}
	}

	return docs
}

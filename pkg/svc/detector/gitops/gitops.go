package gitops

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
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

// CRDetector detects existing GitOps Custom Resources in a source directory.
type CRDetector struct {
	sourceDir string
}

// NewCRDetector creates a new detector for the given source directory.
func NewCRDetector(sourceDir string) *CRDetector {
	return &CRDetector{sourceDir: sourceDir}
}

// FindFluxInstance searches for an existing KSail-managed FluxInstance CR.
// Returns the file path if found, empty string otherwise.
func (d *CRDetector) FindFluxInstance() (string, error) {
	return d.findCR(
		FluxInstanceAPIVersion,
		FluxInstanceKind,
		FluxInstanceDefaultName,
		FluxInstanceNamespace,
	)
}

// FluxDistribution captures the spec.distribution block of a repo-declared
// FluxInstance. Empty fields mean the repo did not specify that value.
type FluxDistribution struct {
	Version  string
	Registry string
	Artifact string
}

// fluxInstanceDoc is the minimal shape needed to read a FluxInstance's
// spec.distribution block from a single YAML document.
type fluxInstanceDoc struct {
	Spec struct {
		Distribution struct {
			Version  string `yaml:"version"`
			Registry string `yaml:"registry"`
			Artifact string `yaml:"artifact"`
		} `yaml:"distribution"`
	} `yaml:"spec"`
}

// FindFluxInstanceDistribution locates a repo-declared KSail-managed FluxInstance
// and returns its spec.distribution block. Returns a zero-value FluxDistribution
// when no FluxInstance is declared (or it omits a distribution block); callers
// treat empty fields as "no override".
func (d *CRDetector) FindFluxInstanceDistribution() (FluxDistribution, error) {
	path, err := d.FindFluxInstance()
	if err != nil {
		return FluxDistribution{}, err
	}

	if path == "" {
		return FluxDistribution{}, nil
	}

	// G304: path comes from FindFluxInstance's WalkDir over the trusted sourceDir.
	data, err := os.ReadFile(path) //nolint:gosec // path validated via WalkDir
	if err != nil {
		return FluxDistribution{}, fmt.Errorf("reading FluxInstance file: %w", err)
	}

	for _, doc := range fsutil.SplitYAMLDocuments(data) {
		if len(doc) == 0 {
			continue
		}

		var probe k8sResource

		if yaml.Unmarshal(doc, &probe) != nil {
			continue
		}

		if !d.isMatchingCR(
			probe,
			FluxInstanceAPIVersion,
			FluxInstanceKind,
			FluxInstanceDefaultName,
			FluxInstanceNamespace,
		) {
			continue
		}

		var parsed fluxInstanceDoc

		if yaml.Unmarshal(doc, &parsed) != nil {
			continue
		}

		return FluxDistribution{
			Version:  parsed.Spec.Distribution.Version,
			Registry: parsed.Spec.Distribution.Registry,
			Artifact: parsed.Spec.Distribution.Artifact,
		}, nil
	}

	return FluxDistribution{}, nil
}

// FindArgoCDApplication searches for an existing KSail-managed ArgoCD Application CR.
// Returns the file path if found, empty string otherwise.
func (d *CRDetector) FindArgoCDApplication() (string, error) {
	return d.findCR(
		ArgoCDApplicationAPIVersion,
		ArgoCDApplicationKind,
		ArgoCDApplicationDefaultName,
		ArgoCDNamespace,
	)
}

// findCR searches recursively for a CR matching the given criteria.
func (d *CRDetector) findCR(
	apiVersion, kind, defaultName, defaultNamespace string,
) (string, error) {
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

		if !fsutil.IsYAMLFile(path) {
			return nil
		}

		match, matchErr := d.checkFile(path, apiVersion, kind, defaultName, defaultNamespace)
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
func (d *CRDetector) checkFile(
	path, apiVersion, kind, defaultName, defaultNamespace string,
) (bool, error) {
	// G304: path is validated via WalkDir starting from trusted sourceDir
	data, err := os.ReadFile(path) //nolint:gosec // path is validated via WalkDir
	if err != nil {
		return false, fmt.Errorf("reading file: %w", err)
	}

	// Handle multi-document YAML files
	docs := fsutil.SplitYAMLDocuments(data)
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}

		var resource k8sResource

		err := yaml.Unmarshal(doc, &resource)
		if err != nil {
			continue
		}

		if d.isMatchingCR(resource, apiVersion, kind, defaultName, defaultNamespace) {
			return true, nil
		}
	}

	return false, nil
}

// isMatchingCR checks if a resource matches the detection criteria.
func (d *CRDetector) isMatchingCR(
	resource k8sResource,
	apiVersion, kind, defaultName, defaultNamespace string,
) bool {
	// Must match apiVersion and kind
	if resource.APIVersion != apiVersion || resource.Kind != kind {
		return false
	}

	// Must live in the namespace KSail manages for this resource type. An
	// explicit namespace that differs is rejected so a same-named CR in another
	// namespace (e.g. a FluxInstance named "flux" outside flux-system) is not
	// mistaken for the KSail-managed one. An omitted namespace is tolerated
	// (the manifest may rely on its Kustomization to set it).
	if resource.Metadata.Namespace != "" && resource.Metadata.Namespace != defaultNamespace {
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

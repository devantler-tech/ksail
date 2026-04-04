package tenant

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	kustomizationgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/kustomization"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// Generate creates a complete tenant directory with all required manifests.
// It orchestrates RBAC + type-specific resource generation, writes files to disk,
// and generates the tenant's kustomization.yaml.
//
// Directory layout: <opts.OutputDir>/<opts.Name>/
//
//	namespace.yaml, serviceaccount.yaml, rolebinding.yaml,
//	kustomization.yaml, [sync.yaml], [project.yaml, app.yaml, argocd-rbac-cm.yaml]
func Generate(opts Options) error {
	opts.ResolveDefaults()
	if err := opts.Validate(); err != nil {
		return err
	}

	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	if _, err := os.Stat(tenantDir); err == nil && !opts.Force {
		return fmt.Errorf("tenant directory %q already exists, use --force to overwrite", tenantDir)
	}

	if err := os.MkdirAll(tenantDir, 0o750); err != nil {
		return fmt.Errorf("creating tenant directory: %w", err)
	}

	// Generate and write RBAC manifests.
	rbacFiles, err := GenerateRBACManifests(opts)
	if err != nil {
		return fmt.Errorf("generating RBAC manifests: %w", err)
	}

	var resources []string
	for filename, content := range rbacFiles {
		if _, err := fsutil.TryWriteFile(content, filepath.Join(tenantDir, filename), opts.Force); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
		resources = append(resources, filename)
	}

	// Generate type-specific manifests.
	switch opts.TenantType {
	case TenantTypeFlux:
		fluxFiles, err := GenerateFluxSyncManifests(opts)
		if err != nil {
			return fmt.Errorf("generating Flux manifests: %w", err)
		}
		for filename, content := range fluxFiles {
			if _, err := fsutil.TryWriteFile(content, filepath.Join(tenantDir, filename), opts.Force); err != nil {
				return fmt.Errorf("writing %s: %w", filename, err)
			}
			resources = append(resources, filename)
		}
	case TenantTypeArgoCD:
		argoFiles, err := GenerateArgoCDManifests(opts)
		if err != nil {
			return fmt.Errorf("generating ArgoCD manifests: %w", err)
		}
		for filename, content := range argoFiles {
			if _, err := fsutil.TryWriteFile(content, filepath.Join(tenantDir, filename), opts.Force); err != nil {
				return fmt.Errorf("writing %s: %w", filename, err)
			}
			resources = append(resources, filename)
		}
	case TenantTypeKubectl:
		// No additional files needed.
	}

	sort.Strings(resources)

	// Generate kustomization.yaml.
	gen := kustomizationgenerator.NewGenerator()
	kustomization := &ktypes.Kustomization{
		Resources: resources,
	}
	if _, err := gen.Generate(kustomization, yamlgenerator.Options{
		Output: filepath.Join(tenantDir, "kustomization.yaml"),
		Force:  opts.Force,
	}); err != nil {
		return fmt.Errorf("generating kustomization.yaml: %w", err)
	}

	return nil
}

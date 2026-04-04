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

	if info, err := os.Stat(tenantDir); err == nil && info.IsDir() {
		if !opts.Force {
			return fmt.Errorf("tenant directory %q already exists, use --force to overwrite", tenantDir)
		}
		// Clean stale files from previous generation (may have been a different type).
		if err := os.RemoveAll(tenantDir); err != nil {
			return fmt.Errorf("removing existing tenant directory: %w", err)
		}
	}

	if err := os.MkdirAll(tenantDir, 0o750); err != nil {
		return fmt.Errorf("creating tenant directory: %w", err)
	}

	// Generate and write RBAC manifests.
	rbacFiles, err := GenerateRBACManifests(opts)
	if err != nil {
		return fmt.Errorf("generating RBAC manifests: %w", err)
	}

	resources, err := writeManifests(tenantDir, rbacFiles, opts.Force)
	if err != nil {
		return err
	}

	// Generate type-specific manifests.
	switch opts.TenantType {
	case TenantTypeFlux:
		fluxFiles, err := GenerateFluxSyncManifests(opts)
		if err != nil {
			return fmt.Errorf("generating Flux manifests: %w", err)
		}
		names, err := writeManifests(tenantDir, fluxFiles, opts.Force)
		if err != nil {
			return err
		}
		resources = append(resources, names...)
	case TenantTypeArgoCD:
		argoFiles, err := GenerateArgoCDManifests(opts)
		if err != nil {
			return fmt.Errorf("generating ArgoCD manifests: %w", err)
		}
		names, err := writeManifests(tenantDir, argoFiles, opts.Force)
		if err != nil {
			return err
		}
		resources = append(resources, names...)
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

// writeManifests writes a map of filename->content to the given directory
// and returns the list of filenames written.
func writeManifests(dir string, files map[string]string, force bool) ([]string, error) {
	names := make([]string, 0, len(files))
	for filename, content := range files {
		if _, err := fsutil.TryWriteFile(content, filepath.Join(dir, filename), force); err != nil {
			return nil, fmt.Errorf("writing %s: %w", filename, err)
		}
		names = append(names, filename)
	}
	return names, nil
}

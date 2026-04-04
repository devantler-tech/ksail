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
//	kustomization.yaml, [sync.yaml], [project.yaml, app.yaml]
func Generate(opts Options) error {
	opts.ResolveDefaults()
	if err := opts.Validate(); err != nil {
		return err
	}

	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	if err := prepareTenantDir(tenantDir, opts.Force); err != nil {
		return err
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
	typeResources, err := generateTypeSpecificManifests(opts, tenantDir)
	if err != nil {
		return err
	}
	resources = append(resources, typeResources...)

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

func prepareTenantDir(tenantDir string, force bool) error {
	if info, err := os.Stat(tenantDir); err == nil && info.IsDir() {
		if !force {
			return fmt.Errorf("%w: %q, use --force to overwrite", ErrTenantAlreadyExists, tenantDir)
		}
		if err := os.RemoveAll(tenantDir); err != nil {
			return fmt.Errorf("removing existing tenant directory: %w", err)
		}
	}
	if err := os.MkdirAll(tenantDir, 0o750); err != nil {
		return fmt.Errorf("creating tenant directory: %w", err)
	}
	return nil
}

func generateTypeSpecificManifests(opts Options, tenantDir string) ([]string, error) {
	switch opts.TenantType {
	case TenantTypeFlux:
		fluxFiles, err := GenerateFluxSyncManifests(opts)
		if err != nil {
			return nil, fmt.Errorf("generating Flux manifests: %w", err)
		}
		return writeManifests(tenantDir, fluxFiles, opts.Force)
	case TenantTypeArgoCD:
		argoFiles, err := GenerateArgoCDManifests(opts)
		if err != nil {
			return nil, fmt.Errorf("generating ArgoCD manifests: %w", err)
		}
		return writeManifests(tenantDir, argoFiles, opts.Force)
	case TenantTypeKubectl:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidTenantType, opts.TenantType)
	}
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

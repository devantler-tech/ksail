package tenant

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	kustomizationgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/kustomization"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
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

	err := opts.Validate()
	if err != nil {
		return err
	}

	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	err = prepareTenantDir(tenantDir, opts.Force)
	if err != nil {
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

	_, err = gen.Generate(kustomization, yamlgenerator.Options{
		Output: filepath.Join(tenantDir, "kustomization.yaml"),
		Force:  opts.Force,
	})
	if err != nil {
		return fmt.Errorf("generating kustomization.yaml: %w", err)
	}

	return nil
}

const tenantDirPermissions = 0o750

func prepareTenantDir(tenantDir string, force bool) error {
	info, statErr := os.Stat(tenantDir)

	switch {
	case statErr != nil && !os.IsNotExist(statErr):
		return fmt.Errorf("checking tenant directory: %w", statErr)
	case statErr == nil:
		if !info.IsDir() {
			return fmt.Errorf(
				"%w: %q exists but is not a directory",
				ErrTenantAlreadyExists, tenantDir,
			)
		}

		if !force {
			return fmt.Errorf(
				"%w: %q, use --force to overwrite",
				ErrTenantAlreadyExists, tenantDir,
			)
		}

		removeErr := os.RemoveAll(tenantDir)
		if removeErr != nil {
			return fmt.Errorf("removing existing tenant directory: %w", removeErr)
		}
	}

	mkdirErr := os.MkdirAll(tenantDir, tenantDirPermissions)
	if mkdirErr != nil {
		return fmt.Errorf("creating tenant directory: %w", mkdirErr)
	}

	return nil
}

func generateTypeSpecificManifests(opts Options, tenantDir string) ([]string, error) {
	switch opts.TenantType {
	case TypeFlux:
		fluxFiles, err := GenerateFluxSyncManifests(opts)
		if err != nil {
			return nil, fmt.Errorf("generating Flux manifests: %w", err)
		}

		return writeManifests(tenantDir, fluxFiles, opts.Force)
	case TypeArgoCD:
		argoFiles, err := GenerateArgoCDManifests(opts)
		if err != nil {
			return nil, fmt.Errorf("generating ArgoCD manifests: %w", err)
		}

		return writeManifests(tenantDir, argoFiles, opts.Force)
	case TypeKubectl:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidType, opts.TenantType)
	}
}

// writeManifests writes a map of filename->content to the given directory
// and returns the list of filenames written.
func writeManifests(dir string, files map[string]string, force bool) ([]string, error) {
	names := make([]string, 0, len(files))
	for filename, content := range files {
		_, writeErr := fsutil.TryWriteFile(content, filepath.Join(dir, filename), force)
		if writeErr != nil {
			return nil, fmt.Errorf("writing %s: %w", filename, writeErr)
		}

		names = append(names, filename)
	}

	return names, nil
}

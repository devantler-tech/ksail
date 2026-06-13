package tenant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant/gitprovider"
	"k8s.io/apimachinery/pkg/util/validation"
)

// DeleteOptions holds configuration for tenant deletion.
type DeleteOptions struct {
	// Name is the tenant name (required).
	Name string
	// OutputDir is the directory containing tenant subdirectories.
	OutputDir string
	// Unregister removes the tenant from kustomization.yaml resources.
	Unregister bool
	// KustomizationPath is the explicit path to kustomization.yaml.
	KustomizationPath string
	// DeleteRepo deletes the tenant's Git repository.
	DeleteRepo bool
	// GitProvider is the Git provider name (github, gitlab, gitea).
	GitProvider string
	// TenantRepo is the tenant repo as owner/repo-name.
	TenantRepo string
	// GitToken is the Git provider API token.
	GitToken string
}

// Delete removes a tenant's manifests and optionally unregisters and deletes the repo.
func Delete(ctx context.Context, opts DeleteOptions) error {
	err := validateDeleteOpts(opts)
	if err != nil {
		return err
	}

	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	_, statErr := os.Stat(tenantDir)
	if os.IsNotExist(statErr) {
		return fmt.Errorf("%w: %q", ErrTenantDirNotExist, tenantDir)
	}

	// Detect ArgoCD tenant before deletion so we can clean up RBAC.
	argoCD := isArgoCDTenant(tenantDir)

	if opts.Unregister {
		unregErr := UnregisterTenant(opts.Name, opts.OutputDir, opts.KustomizationPath)
		if unregErr != nil {
			if !errors.Is(unregErr, ErrKustomizationNotFound) {
				return fmt.Errorf("unregister tenant: %w", unregErr)
			}
		}
	}

	if argoCD {
		rbacErr := cleanupArgoCDRBAC(opts.OutputDir, opts.Name)
		if rbacErr != nil {
			return fmt.Errorf("cleanup ArgoCD RBAC: %w", rbacErr)
		}
	}

	removeErr := os.RemoveAll(tenantDir)
	if removeErr != nil {
		return fmt.Errorf("remove tenant directory: %w", removeErr)
	}

	if opts.DeleteRepo {
		return deleteRepo(ctx, opts)
	}

	return nil
}

func validateDeleteOpts(opts DeleteOptions) error {
	if opts.Name == "" {
		return ErrTenantNameRequired
	}

	if errs := validation.IsDNS1123Label(opts.Name); len(errs) > 0 {
		return fmt.Errorf(
			"%w: %s (%s)",
			ErrInvalidTenantName,
			opts.Name,
			strings.Join(errs, "; "),
		)
	}

	if opts.DeleteRepo {
		if opts.GitProvider == "" {
			return fmt.Errorf("%w", ErrDeleteRepoGitProviderRequired)
		}

		if opts.TenantRepo == "" {
			return fmt.Errorf("%w", ErrDeleteRepoTenantRepoRequired)
		}
	}

	return nil
}

func deleteRepo(ctx context.Context, opts DeleteOptions) error {
	if opts.GitProvider == "" {
		return fmt.Errorf("%w", ErrDeleteRepoGitProviderRequired)
	}

	if opts.TenantRepo == "" {
		return fmt.Errorf("%w", ErrDeleteRepoTenantRepoRequired)
	}

	owner, repo, err := gitprovider.ParseOwnerRepo(opts.TenantRepo)
	if err != nil {
		return fmt.Errorf("parse tenant-repo: %w", err)
	}

	token := gitprovider.ResolveToken(opts.GitProvider, opts.GitToken)

	provider, err := gitprovider.New(opts.GitProvider, token)
	if err != nil {
		return fmt.Errorf("create git provider: %w", err)
	}

	err = provider.DeleteRepo(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("delete git repo: %w", err)
	}

	return nil
}

// isArgoCDTenant checks whether the tenant directory contains an ArgoCD AppProject
// (project.yaml), indicating this is an ArgoCD-managed tenant.
func isArgoCDTenant(tenantDir string) bool {
	projectPath := filepath.Join(tenantDir, "project.yaml")

	_, err := os.Stat(projectPath)

	return err == nil
}

// cleanupArgoCDRBAC removes the tenant's policy lines from the argocd-rbac-cm ConfigMap.
// The ConfigMap is discovered by scanning YAML files in outputDir for a Kubernetes ConfigMap
// with metadata.name "argocd-rbac-cm" (content-based, not filename-based).
// Handles multi-document YAML files where the ConfigMap may not be the first document.
// Skips silently if no matching file is found; propagates real I/O errors.
//
// Find and removal both route through the shared rbaccm.go pipeline so that
// tenant.Delete and the CLI tenant commands stay in lock-step.
func cleanupArgoCDRBAC(outputDir, tenantName string) error {
	rbacPath, err := FindArgoCDRBACCM(outputDir)
	if err != nil {
		return fmt.Errorf("scanning for %s: %w", rbacConfigMapName, err)
	}

	if rbacPath == "" {
		return nil
	}

	err = RemoveArgoCDRBACPolicyFile(rbacPath, tenantName)
	if err != nil {
		return fmt.Errorf("remove RBAC policy: %w", err)
	}

	return nil
}

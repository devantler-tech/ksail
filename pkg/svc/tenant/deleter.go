package tenant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant/gitprovider"
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
	// GitRepo is the tenant repo as owner/repo-name.
	GitRepo string
	// GitToken is the Git provider API token.
	GitToken string
}

// Delete removes a tenant's manifests and optionally unregisters and deletes the repo.
func Delete(opts DeleteOptions) error {
	if opts.Name == "" {
		return ErrTenantNameRequired
	}
	if strings.Contains(opts.Name, "..") || strings.ContainsAny(opts.Name, `/\`) {
		return fmt.Errorf("%w: %q must not contain path separators or '..'", ErrInvalidTenantName, opts.Name)
	}

	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	_, statErr := os.Stat(tenantDir)
	if os.IsNotExist(statErr) {
		return fmt.Errorf("%w: %q", ErrTenantDirNotExist, tenantDir)
	}

	if opts.Unregister {
		if err := UnregisterTenant(opts.Name, opts.OutputDir, opts.KustomizationPath); err != nil {
			if !errors.Is(err, ErrKustomizationNotFound) {
				return fmt.Errorf("unregister tenant: %w", err)
			}
		}
	}

	if err := os.RemoveAll(tenantDir); err != nil {
		return fmt.Errorf("remove tenant directory: %w", err)
	}

	if opts.DeleteRepo {
		return deleteRepo(opts)
	}

	return nil
}

func deleteRepo(opts DeleteOptions) error {
	if opts.GitProvider == "" {
		return fmt.Errorf("%w", ErrDeleteRepoGitProviderRequired)
	}
	if opts.GitRepo == "" {
		return fmt.Errorf("%w", ErrDeleteRepoGitRepoRequired)
	}

	owner, repo, err := gitprovider.ParseOwnerRepo(opts.GitRepo)
	if err != nil {
		return fmt.Errorf("parse git-repo: %w", err)
	}

	token := gitprovider.ResolveToken(opts.GitProvider, opts.GitToken)
	provider, err := gitprovider.New(opts.GitProvider, token)
	if err != nil {
		return fmt.Errorf("create git provider: %w", err)
	}

	err = provider.DeleteRepo(context.Background(), owner, repo)
	if err != nil {
		return fmt.Errorf("delete git repo: %w", err)
	}

	return nil
}

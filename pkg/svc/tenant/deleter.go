package tenant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant/gitprovider"
)

// DeleteOptions holds configuration for tenant deletion.
type DeleteOptions struct {
	// Name is the tenant name (required).
	Name string
	// OutputDir is the directory containing tenant subdirectories.
	OutputDir string
	// Force allows deletion without extra checks.
	Force bool
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
	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	if _, err := os.Stat(tenantDir); os.IsNotExist(err) {
		return fmt.Errorf("tenant directory %q does not exist", tenantDir)
	}

	if opts.Unregister {
		if err := UnregisterTenant(opts.Name, opts.OutputDir, opts.KustomizationPath); err != nil {
			if !errors.Is(err, ErrKustomizationNotFound) {
				return fmt.Errorf("unregister tenant: %w", err)
			}
			// kustomization.yaml not found — continue with deletion
		}
	}

	if err := os.RemoveAll(tenantDir); err != nil {
		return fmt.Errorf("remove tenant directory: %w", err)
	}

	if opts.DeleteRepo {
		if opts.GitProvider == "" {
			return fmt.Errorf("--git-provider is required when --delete-repo is set")
		}
		if opts.GitRepo == "" {
			return fmt.Errorf("--git-repo is required when --delete-repo is set")
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

		if err := provider.DeleteRepo(context.Background(), owner, repo); err != nil {
			return fmt.Errorf("delete git repo: %w", err)
		}
	}

	return nil
}

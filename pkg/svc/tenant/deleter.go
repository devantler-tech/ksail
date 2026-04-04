package tenant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant/gitprovider"
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
	// GitRepo is the tenant repo as owner/repo-name.
	GitRepo string
	// GitToken is the Git provider API token.
	GitToken string
}

// Delete removes a tenant's manifests and optionally unregisters and deletes the repo.
func Delete(ctx context.Context, opts DeleteOptions) error {
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

	tenantDir := filepath.Join(opts.OutputDir, opts.Name)

	_, statErr := os.Stat(tenantDir)
	if os.IsNotExist(statErr) {
		return fmt.Errorf("%w: %q", ErrTenantDirNotExist, tenantDir)
	}

	if opts.Unregister {
		unregErr := UnregisterTenant(opts.Name, opts.OutputDir, opts.KustomizationPath)
		if unregErr != nil {
			if !errors.Is(unregErr, ErrKustomizationNotFound) {
				return fmt.Errorf("unregister tenant: %w", unregErr)
			}
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

func deleteRepo(ctx context.Context, opts DeleteOptions) error {
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

	err = provider.DeleteRepo(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("delete git repo: %w", err)
	}

	return nil
}

package tenant

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant/gitprovider"
)

// DeliverPROptions holds options for PR delivery.
type DeliverPROptions struct {
	// GitProvider is the git provider name (e.g. "github").
	GitProvider string
	// GitToken is the explicit API token (optional; resolved via ResolveToken if empty).
	GitToken string
	// PlatformRepo is the platform repo as owner/repo-name (optional; auto-detected if empty).
	PlatformRepo string
	// TargetBranch is the PR target branch (optional; uses repo default branch if empty).
	TargetBranch string
	// TenantName is the tenant name.
	TenantName string
	// OutputDir is the directory where tenant manifests were generated.
	OutputDir string
	// KustomizationPath is the explicit path to kustomization.yaml (optional; auto-discovers if empty).
	KustomizationPath string
}

// DeliverPR orchestrates delivering tenant changes as a pull request:
//  1. Resolves the platform repo (explicit or auto-detected from git remote)
//  2. Determines the target branch (explicit or repo default)
//  3. Collects generated/modified files from disk
//  4. Creates a feature branch, pushes files, and opens a PR
//
// Returns the PR URL on success.
func DeliverPR(ctx context.Context, opts DeliverPROptions) (string, error) {
	// Resolve platform repo.
	platformRepo := opts.PlatformRepo
	if platformRepo == "" {
		detected, err := DetectPlatformRepo(opts.OutputDir)
		if err != nil {
			return "", fmt.Errorf("auto-detecting platform repo: %w", err)
		}

		platformRepo = detected
	}

	owner, repoName, err := gitprovider.ParseOwnerRepo(platformRepo)
	if err != nil {
		return "", fmt.Errorf("parsing platform-repo: %w", err)
	}

	// Resolve token and create provider.
	token := gitprovider.ResolveToken(opts.GitProvider, opts.GitToken)
	if token == "" {
		return "", fmt.Errorf(
			"%w: set --git-token or the %s environment variable",
			gitprovider.ErrTokenRequired,
			strings.ToUpper(opts.GitProvider)+"_TOKEN",
		)
	}

	provider, err := gitprovider.New(opts.GitProvider, token)
	if err != nil {
		return "", fmt.Errorf("creating git provider: %w", err)
	}

	// Determine target branch.
	targetBranch := opts.TargetBranch
	if targetBranch == "" {
		defaultBranch, branchErr := provider.GetDefaultBranch(ctx, owner, repoName)
		if branchErr != nil {
			return "", fmt.Errorf("getting default branch: %w", branchErr)
		}

		targetBranch = defaultBranch
	}

	// Collect files from disk.
	repoRoot, err := findGitRoot(opts.OutputDir)
	if err != nil {
		return "", fmt.Errorf("finding git root: %w", err)
	}

	kustomizationPath := opts.KustomizationPath
	if kustomizationPath == "" {
		found, findErr := FindKustomization(opts.OutputDir)
		if findErr != nil {
			return "", fmt.Errorf("finding kustomization.yaml: %w", findErr)
		}

		kustomizationPath = found
	}

	files, err := CollectDeliveryFiles(opts.TenantName, opts.OutputDir, kustomizationPath, repoRoot)
	if err != nil {
		return "", fmt.Errorf("collecting delivery files: %w", err)
	}

	// Create feature branch.
	featureBranch := "ksail/tenant/add-" + opts.TenantName

	err = provider.CreateBranch(ctx, owner, repoName, featureBranch, targetBranch)
	if err != nil {
		return "", fmt.Errorf("creating feature branch: %w", err)
	}

	// Push files to the feature branch.
	commitMsg := "feat(tenant): add " + opts.TenantName

	err = provider.PushFilesToBranch(ctx, owner, repoName, featureBranch, files, commitMsg)
	if err != nil {
		return "", fmt.Errorf("pushing files to branch: %w", err)
	}

	// Create the pull request.
	prURL, err := provider.CreatePullRequest(ctx, owner, repoName, gitprovider.PROptions{
		Title: commitMsg,
		Body:  fmt.Sprintf("Adds tenant `%s` manifests and registers in kustomization.yaml.", opts.TenantName),
		Head:  featureBranch,
		Base:  targetBranch,
	})
	if err != nil {
		return "", fmt.Errorf("creating pull request: %w", err)
	}

	return prURL, nil
}

// CollectDeliveryFiles reads generated tenant files and the kustomization.yaml
// from disk, returning a map of repo-relative paths to file contents.
func CollectDeliveryFiles(
	tenantName, outputDir, kustomizationPath, repoRoot string,
) (map[string][]byte, error) {
	files := make(map[string][]byte)

	// Collect tenant directory files.
	tenantDir := filepath.Join(outputDir, tenantName)

	err := filepath.WalkDir(tenantDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		content, readErr := os.ReadFile(path) //nolint:gosec // path from WalkDir within known dir
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}

		relPath, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return fmt.Errorf("computing relative path for %s: %w", path, relErr)
		}

		files[filepath.ToSlash(relPath)] = content

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking tenant directory: %w", err)
	}

	// Collect kustomization.yaml.
	kContent, err := os.ReadFile(kustomizationPath) //nolint:gosec // path already resolved
	if err != nil {
		return nil, fmt.Errorf("reading kustomization.yaml: %w", err)
	}

	kRelPath, err := filepath.Rel(repoRoot, kustomizationPath)
	if err != nil {
		return nil, fmt.Errorf("computing relative path for kustomization.yaml: %w", err)
	}

	files[filepath.ToSlash(kRelPath)] = kContent

	return files, nil
}

// sshRemotePattern matches SSH git remote URLs (e.g. git@github.com:owner/repo.git).
var sshRemotePattern = regexp.MustCompile(`^[^@]+@[^:]+:([^/]+/[^/]+?)(?:\.git)?$`)

// httpsRemotePattern matches HTTPS git remote URLs (e.g. https://github.com/owner/repo.git).
var httpsRemotePattern = regexp.MustCompile(`^https?://[^/]+/([^/]+/[^/]+?)(?:\.git)?/?$`)

// DetectPlatformRepo extracts the owner/repo from the git remote of the
// directory containing the generated manifests.
func DetectPlatformRepo(dir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin") //nolint:gosec // static command
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf(
			"%w: could not detect platform repo from git remote in %s",
			ErrPlatformRepoRequired, dir,
		)
	}

	remoteURL := strings.TrimSpace(string(out))

	return ParseRemoteURL(remoteURL)
}

// ParseRemoteURL extracts owner/repo from a git remote URL.
// Supports SSH (git@host:owner/repo.git) and HTTPS (https://host/owner/repo.git) formats.
func ParseRemoteURL(remoteURL string) (string, error) {
	if matches := sshRemotePattern.FindStringSubmatch(remoteURL); len(matches) == 2 {
		return matches[1], nil
	}

	if matches := httpsRemotePattern.FindStringSubmatch(remoteURL); len(matches) == 2 {
		return matches[1], nil
	}

	return "", fmt.Errorf(
		"%w: unrecognized git remote format %q",
		ErrPlatformRepoRequired, remoteURL,
	)
}

// findGitRoot returns the top-level directory of the git repository
// containing the given directory.
func findGitRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel") //nolint:gosec // static command
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

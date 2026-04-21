package tenant

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant/gitprovider"
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
	// Register indicates whether kustomization.yaml was modified and should be included.
	Register bool
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
	owner, repoName, err := resolvePlatformRepo(ctx, opts)
	if err != nil {
		return "", err
	}

	provider, err := resolveProvider(opts)
	if err != nil {
		return "", err
	}

	targetBranch, err := resolveTargetBranch(ctx, provider, owner, repoName, opts.TargetBranch)
	if err != nil {
		return "", err
	}

	files, err := collectFiles(ctx, opts)
	if err != nil {
		return "", err
	}

	return createPRWithFiles(ctx, provider, owner, repoName, targetBranch, opts.TenantName, files)
}

func resolvePlatformRepo(
	ctx context.Context, opts DeliverPROptions,
) (string, string, error) {
	platformRepo := opts.PlatformRepo
	if platformRepo == "" {
		detected, err := DetectPlatformRepo(ctx, opts.OutputDir)
		if err != nil {
			return "", "", fmt.Errorf("auto-detecting platform repo: %w", err)
		}

		platformRepo = detected
	}

	owner, repoName, err := gitprovider.ParseOwnerRepo(platformRepo)
	if err != nil {
		return "", "", fmt.Errorf("parsing platform-repo: %w", err)
	}

	return owner, repoName, nil
}

func resolveProvider(opts DeliverPROptions) (gitprovider.Provider, error) {
	token := gitprovider.ResolveToken(opts.GitProvider, opts.GitToken)
	if token == "" {
		envHint := strings.ToUpper(opts.GitProvider) + "_TOKEN"
		if strings.EqualFold(opts.GitProvider, "github") {
			envHint = "GH_TOKEN, GITHUB_TOKEN"
		}

		return nil, fmt.Errorf(
			"%w: set --git-token or one of the %s environment variables",
			gitprovider.ErrTokenRequired,
			envHint,
		)
	}

	provider, err := gitprovider.New(opts.GitProvider, token)
	if err != nil {
		return nil, fmt.Errorf("creating git provider: %w", err)
	}

	return provider, nil
}

func resolveTargetBranch(
	ctx context.Context,
	provider gitprovider.Provider,
	owner, repoName, explicit string,
) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	branch, err := provider.GetDefaultBranch(ctx, owner, repoName)
	if err != nil {
		return "", fmt.Errorf("getting default branch: %w", err)
	}

	return branch, nil
}

func collectFiles(ctx context.Context, opts DeliverPROptions) (map[string][]byte, error) {
	repoRoot, err := findGitRoot(ctx, opts.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("finding git root: %w", err)
	}

	files, err := collectTenantFiles(opts.TenantName, opts.OutputDir, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("collecting tenant files: %w", err)
	}

	if opts.Register {
		kErr := collectKustomizationFile(files, opts.OutputDir, opts.KustomizationPath, repoRoot)
		if kErr != nil {
			return nil, kErr
		}
	}

	return files, nil
}

func collectKustomizationFile(
	files map[string][]byte,
	outputDir, kustomizationPath, repoRoot string,
) error {
	if kustomizationPath == "" {
		found, err := FindKustomization(outputDir)
		if err != nil {
			return fmt.Errorf("finding kustomization.yaml: %w", err)
		}

		kustomizationPath = found
	}

	kContent, err := fsutil.ReadFileSafe(repoRoot, kustomizationPath)
	if err != nil {
		return fmt.Errorf("reading kustomization.yaml: %w", err)
	}

	kRelPath, err := safeRelPath(repoRoot, kustomizationPath)
	if err != nil {
		return err
	}

	files[filepath.ToSlash(kRelPath)] = kContent

	return nil
}

func createPRWithFiles(
	ctx context.Context,
	provider gitprovider.Provider,
	owner, repoName, targetBranch, tenantName string,
	files map[string][]byte,
) (string, error) {
	featureBranch := "ksail/tenant/add-" + tenantName
	commitMsg := "feat(tenant): add " + tenantName

	err := provider.CreateBranch(ctx, owner, repoName, featureBranch, targetBranch)
	if err != nil {
		return "", fmt.Errorf("creating feature branch: %w", err)
	}

	err = provider.PushFilesToBranch(ctx, owner, repoName, featureBranch, files, commitMsg)
	if err != nil {
		return "", fmt.Errorf("pushing files to branch: %w", err)
	}

	prURL, err := provider.CreatePullRequest(ctx, owner, repoName, gitprovider.PROptions{
		Title: commitMsg,
		Body:  fmt.Sprintf("Adds tenant `%s` manifests.", tenantName),
		Head:  featureBranch,
		Base:  targetBranch,
	})
	if err != nil {
		return "", fmt.Errorf("creating pull request: %w", err)
	}

	return prURL, nil
}

// CollectDeliveryFiles reads generated tenant files and optionally kustomization.yaml
// from disk, returning a map of repo-relative paths to file contents.
func CollectDeliveryFiles(
	tenantName, outputDir, kustomizationPath, repoRoot string,
) (map[string][]byte, error) {
	files, err := collectTenantFiles(tenantName, outputDir, repoRoot)
	if err != nil {
		return nil, err
	}

	if kustomizationPath != "" {
		kErr := collectKustomizationFile(files, outputDir, kustomizationPath, repoRoot)
		if kErr != nil {
			return nil, kErr
		}
	}

	return files, nil
}

func collectTenantFiles(
	tenantName, outputDir, repoRoot string,
) (map[string][]byte, error) {
	files := make(map[string][]byte)
	tenantDir := filepath.Join(outputDir, tenantName)

	err := filepath.WalkDir(tenantDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		content, readErr := fsutil.ReadFileSafe(repoRoot, path)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}

		relPath, relErr := safeRelPath(repoRoot, path)
		if relErr != nil {
			return relErr
		}

		files[filepath.ToSlash(relPath)] = content

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking tenant directory: %w", err)
	}

	return files, nil
}

// safeRelPath computes a relative path from base to target and rejects
// paths that escape the base directory.
func safeRelPath(base, target string) (string, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", fmt.Errorf("computing relative path for %s: %w", target, err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"%w: %q is outside repo root %q",
			ErrOutsideRepoRoot, target, base,
		)
	}

	return rel, nil
}

// sshRemotePattern matches SCP-style SSH git remote URLs (e.g. git@github.com:owner/repo.git).
var sshRemotePattern = regexp.MustCompile(`^[^@]+@[^:]+:([^/]+/[^/]+?)(?:\.git)?$`)

// sshURLPattern matches ssh:// style git remote URLs (e.g. ssh://git@github.com/owner/repo.git).
var sshURLPattern = regexp.MustCompile(`^ssh://[^@]+@[^/]+/([^/]+/[^/]+?)(?:\.git)?/?$`)

// httpsRemotePattern matches HTTPS git remote URLs (e.g. https://github.com/owner/repo.git).
var httpsRemotePattern = regexp.MustCompile(`^https?://[^/]+/([^/]+/[^/]+?)(?:\.git)?/?$`)

// remotePatternExpectedGroups is the number of expected match groups for remote URL patterns.
const remotePatternExpectedGroups = 2

// DetectPlatformRepo extracts the owner/repo from the git remote of the
// directory containing the generated manifests.
func DetectPlatformRepo(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
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
// Supports SCP-style SSH (git@host:owner/repo.git), ssh:// URLs, and HTTPS formats.
func ParseRemoteURL(remoteURL string) (string, error) {
	if matches := sshRemotePattern.FindStringSubmatch(
		remoteURL,
	); len(
		matches,
	) == remotePatternExpectedGroups {
		return matches[1], nil
	}

	if matches := sshURLPattern.FindStringSubmatch(
		remoteURL,
	); len(
		matches,
	) == remotePatternExpectedGroups {
		return matches[1], nil
	}

	if matches := httpsRemotePattern.FindStringSubmatch(
		remoteURL,
	); len(
		matches,
	) == remotePatternExpectedGroups {
		return matches[1], nil
	}

	return "", fmt.Errorf(
		"%w: unrecognized git remote format %q",
		ErrPlatformRepoRequired, remoteURL,
	)
}

// findGitRoot returns the top-level directory of the git repository
// containing the given directory.
func findGitRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

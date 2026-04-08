package gitprovider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	ghauth "github.com/cli/go-gh/v2/pkg/auth"
)

// RepoVisibility defines the visibility of a Git repository.
type RepoVisibility string

const (
	// VisibilityPrivate creates a private repository.
	VisibilityPrivate RepoVisibility = "Private"
	// VisibilityInternal creates an internal repository (org-visible).
	VisibilityInternal RepoVisibility = "Internal"
	// VisibilityPublic creates a public repository.
	VisibilityPublic RepoVisibility = "Public"
)

// Provider is the interface for Git provider operations.
type Provider interface {
	// CreateRepo creates a new repository.
	CreateRepo(ctx context.Context, owner, name string, visibility RepoVisibility) error
	// PushFiles pushes files to a repository's default branch (one commit per file via Contents API).
	PushFiles(
		ctx context.Context,
		owner, name string,
		files map[string][]byte,
		commitMsg string,
	) error
	// DeleteRepo deletes a repository.
	DeleteRepo(ctx context.Context, owner, name string) error
	// GetDefaultBranch returns the default branch name of a repository.
	GetDefaultBranch(ctx context.Context, owner, repo string) (string, error)
	// CreateBranch creates a new branch from the given base branch.
	CreateBranch(ctx context.Context, owner, repo, branchName, baseBranch string) error
	// PushFilesToBranch pushes files to a specific branch (one commit per file via Contents API).
	PushFilesToBranch(
		ctx context.Context,
		owner, repo, branch string,
		files map[string][]byte,
		commitMsg string,
	) error
	// CreatePullRequest creates a pull request and returns the PR URL.
	CreatePullRequest(ctx context.Context, owner, repo string, opts PROptions) (string, error)
}

// PROptions holds options for creating a pull request.
type PROptions struct {
	// Title is the pull request title.
	Title string
	// Body is the pull request description.
	Body string
	// Head is the source branch name.
	Head string
	// Base is the target branch name.
	Base string
}

var (
	// ErrUnsupportedProvider is returned when an unsupported Git provider is specified.
	ErrUnsupportedProvider = errors.New("unsupported git provider")
	// ErrTokenRequired is returned when a token is required but not provided.
	ErrTokenRequired = errors.New("git provider API token is required")
	// ErrInvalidGitRepoFormat is returned when the git-repo format is invalid.
	ErrInvalidGitRepoFormat = errors.New("invalid git-repo format")
	// ErrInvalidRepoVisibility is returned when the repo visibility is invalid.
	ErrInvalidRepoVisibility = errors.New("invalid repo-visibility")
	// ErrGitHubAPI is returned when the GitHub API returns an error.
	ErrGitHubAPI = errors.New("GitHub API error")
	// ErrRepoAlreadyExists is returned when the repository already exists.
	ErrRepoAlreadyExists = errors.New("repository already exists")
	// ErrOwnerMismatch is returned when the token user does not match the requested owner.
	ErrOwnerMismatch = errors.New("authenticated user does not match requested owner")
	// ErrBranchAlreadyExists is returned when the branch already exists.
	ErrBranchAlreadyExists = errors.New("branch already exists")
)

const (
	// providerGitHub is the provider name for GitHub.
	providerGitHub = "github"
	// providerGitLab is the provider name for GitLab.
	providerGitLab = "gitlab"
	// providerGitea is the provider name for Gitea.
	providerGitea = "gitea"
)

// New creates a Provider for the given provider name.
// Supported: "github". Returns error for unsupported providers.
func New(providerName, token string) (Provider, error) {
	if token == "" {
		return nil, ErrTokenRequired
	}

	switch strings.ToLower(providerName) {
	case providerGitHub:
		return newGitHubProvider(token), nil
	default:
		return nil, fmt.Errorf("%w: %s (supported: github)", ErrUnsupportedProvider, providerName)
	}
}

// resolveGitHubToken resolves a GitHub token using the go-gh SDK.
// It checks GH_TOKEN/GITHUB_TOKEN env vars and the GitHub CLI config (hosts.yml).
//
//nolint:gochecknoglobals // dependency injection for tests
var resolveGitHubToken = func() string {
	token, _ := ghauth.TokenFromEnvOrConfig("github.com")

	return token
}

// ResolveToken resolves the API token using the fallback chain:
// 1. Explicit token parameter (--git-token flag)
// 2. Provider SDK auto-detection:
//   - GitHub: go-gh SDK (checks GH_TOKEN, GITHUB_TOKEN env vars and GitHub CLI config)
//   - GitLab: GITLAB_TOKEN env var
//   - Gitea: GITEA_TOKEN env var
func ResolveToken(providerName, explicitToken string) string {
	if explicitToken != "" {
		return explicitToken
	}

	switch strings.ToLower(providerName) {
	case providerGitHub:
		return resolveGitHubToken()
	case providerGitLab:
		return os.Getenv("GITLAB_TOKEN")
	case providerGitea:
		return os.Getenv("GITEA_TOKEN")
	default:
		return ""
	}
}

const gitRepoSplitParts = 3

// ParseOwnerRepo splits "owner/repo-name" into owner and repo.
func ParseOwnerRepo(gitRepo string) (string, string, error) {
	parts := strings.SplitN(gitRepo, "/", gitRepoSplitParts)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf(
			"%w: %q (expected owner/repo-name)",
			ErrInvalidGitRepoFormat,
			gitRepo,
		)
	}

	return parts[0], parts[1], nil
}

// ParseVisibility validates and normalizes a repo visibility string (case-insensitive).
func ParseVisibility(value string) (RepoVisibility, error) {
	switch strings.ToLower(value) {
	case "private":
		return VisibilityPrivate, nil
	case "internal":
		return VisibilityInternal, nil
	case "public":
		return VisibilityPublic, nil
	default:
		return "", fmt.Errorf(
			"%w %q: must be Private, Internal, or Public",
			ErrInvalidRepoVisibility,
			value,
		)
	}
}

// ResolveProviderHost maps a git provider name to its hostname.
// Unknown providers are returned as-is (assumed to be a custom host).
func ResolveProviderHost(provider string) string {
	switch strings.ToLower(provider) {
	case providerGitHub:
		return "github.com"
	case providerGitLab:
		return "gitlab.com"
	case providerGitea:
		return "gitea.com"
	default:
		return provider
	}
}

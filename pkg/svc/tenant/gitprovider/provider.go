package gitprovider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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
	// PushFiles pushes files to a repository (creates initial commit or updates).
	PushFiles(ctx context.Context, owner, name string, files map[string][]byte, commitMsg string) error
	// DeleteRepo deletes a repository.
	DeleteRepo(ctx context.Context, owner, name string) error
}

var (
	// ErrUnsupportedProvider is returned when an unsupported Git provider is specified.
	ErrUnsupportedProvider = errors.New("unsupported git provider")
	// ErrTokenRequired is returned when a token is required but not provided.
	ErrTokenRequired = errors.New("git provider API token is required")
)

// New creates a Provider for the given provider name.
// Supported: "github". Returns error for unsupported providers.
func New(providerName, token string) (Provider, error) {
	if token == "" {
		return nil, ErrTokenRequired
	}
	switch strings.ToLower(providerName) {
	case "github":
		return newGitHubProvider(token), nil
	default:
		return nil, fmt.Errorf("%w: %s (supported: github)", ErrUnsupportedProvider, providerName)
	}
}

// ResolveToken resolves the API token using the fallback chain:
// 1. Explicit token parameter
// 2. Environment variable (GITHUB_TOKEN, GITLAB_TOKEN, etc.)
// 3. Empty string (let caller decide)
func ResolveToken(providerName, explicitToken string) string {
	if explicitToken != "" {
		return explicitToken
	}
	switch strings.ToLower(providerName) {
	case "github":
		return os.Getenv("GITHUB_TOKEN")
	case "gitlab":
		return os.Getenv("GITLAB_TOKEN")
	case "gitea":
		return os.Getenv("GITEA_TOKEN")
	default:
		return ""
	}
}

// ParseOwnerRepo splits "owner/repo-name" into owner and repo.
func ParseOwnerRepo(gitRepo string) (owner, repo string, err error) {
	parts := strings.SplitN(gitRepo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid git-repo format: %q (expected owner/repo-name)", gitRepo)
	}
	return parts[0], parts[1], nil
}

package gitprovider

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/go-github/v72/github"
)

const userAgent = "ksail"

const (
	visibilityPublicStr   = "public"
	visibilityInternalStr = "internal"
	visibilityPrivateStr  = "private"
)

type gitHubProvider struct {
	client *github.Client
}

func newGitHubProvider(token string) *gitHubProvider {
	client := github.NewClient(nil).WithAuthToken(token)
	client.UserAgent = userAgent

	return &gitHubProvider{client: client}
}

// CreateRepo creates a GitHub repository.
// If owner is an org, creates under /orgs/{owner}/repos.
// Otherwise falls back to /user/repos.
func (g *gitHubProvider) CreateRepo(
	ctx context.Context,
	owner, name string,
	visibility RepoVisibility,
) error {
	isPrivate, visStr := resolveVisibility(visibility)

	repo := &github.Repository{
		Name:       new(name),
		Private:    new(isPrivate),
		AutoInit:   new(false),
		Visibility: new(visStr),
	}

	// Try org endpoint first.
	_, resp, err := g.client.Repositories.Create(ctx, owner, repo)
	if err == nil {
		return nil
	}

	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return g.createUserRepo(ctx, owner, name, visibility, repo)
	}

	return g.classifyCreateError(err, owner, name)
}

// PushFiles pushes files to a repository's default branch using the Contents API.
func (g *gitHubProvider) PushFiles(
	ctx context.Context,
	owner, name string,
	files map[string][]byte,
	commitMsg string,
) error {
	return g.pushFilesInternal(ctx, owner, name, "", files, commitMsg)
}

// PushFilesToBranch pushes files to a specific branch using the Contents API.
func (g *gitHubProvider) PushFilesToBranch(
	ctx context.Context,
	owner, repo, branch string,
	files map[string][]byte,
	commitMsg string,
) error {
	return g.pushFilesInternal(ctx, owner, repo, branch, files, commitMsg)
}

// DeleteRepo deletes a GitHub repository.
func (g *gitHubProvider) DeleteRepo(ctx context.Context, owner, name string) error {
	_, err := g.client.Repositories.Delete(ctx, owner, name)
	if err != nil {
		return fmt.Errorf(
			"%w during delete repository: %w",
			ErrGitHubAPI,
			err,
		)
	}

	return nil
}

// GetDefaultBranch returns the default branch name of a repository.
func (g *gitHubProvider) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, _, err := g.client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return "", fmt.Errorf("get repository: %w", err)
	}

	return r.GetDefaultBranch(), nil
}

// CreateBranch creates a new branch from the given base branch.
func (g *gitHubProvider) CreateBranch(
	ctx context.Context,
	owner, repo, branchName, baseBranch string,
) error {
	baseRef, _, err := g.client.Git.GetRef(ctx, owner, repo, "refs/heads/"+baseBranch)
	if err != nil {
		return fmt.Errorf("get base branch %q: %w", baseBranch, err)
	}

	newRef := &github.Reference{
		Ref:    new("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: baseRef.Object.SHA},
	}

	_, resp, err := g.client.Git.CreateRef(ctx, owner, repo, newRef)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity &&
			strings.Contains(err.Error(), "Reference already exists") {
			return fmt.Errorf("%w: %s", ErrBranchAlreadyExists, branchName)
		}

		return fmt.Errorf("create branch %q: %w", branchName, err)
	}

	return nil
}

// CreatePullRequest creates a pull request and returns the PR URL.
func (g *gitHubProvider) CreatePullRequest(
	ctx context.Context,
	owner, repo string,
	opts PROptions,
) (string, error) {
	pullRequest, _, err := g.client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title: new(opts.Title),
		Body:  new(opts.Body),
		Head:  new(opts.Head),
		Base:  new(opts.Base),
	})
	if err != nil {
		return "", fmt.Errorf("create pull request: %w", err)
	}

	return pullRequest.GetHTMLURL(), nil
}

func (g *gitHubProvider) pushFilesInternal(
	ctx context.Context,
	owner, repo, branch string,
	files map[string][]byte,
	commitMsg string,
) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	for _, path := range paths {
		content := files[path]

		opts := &github.RepositoryContentFileOptions{
			Message: new(commitMsg),
			Content: content,
		}

		if branch != "" {
			opts.Branch = new(branch)
		}

		// Check if file already exists to get its SHA (required for updates).
		sha, err := g.getFileSHA(ctx, owner, repo, path, branch)
		if err != nil {
			return fmt.Errorf("checking file %s: %w", path, err)
		}

		if sha != "" {
			opts.SHA = new(sha)
		}

		_, _, err = g.client.Repositories.CreateFile(ctx, owner, repo, path, opts)
		if err != nil {
			return fmt.Errorf("push file %s: %w", path, err)
		}
	}

	return nil
}

func (g *gitHubProvider) getFileSHA(
	ctx context.Context, owner, repo, path, branch string,
) (string, error) {
	getOpts := &github.RepositoryContentGetOptions{}
	if branch != "" {
		getOpts.Ref = branch
	}

	fileContent, _, resp, err := g.client.Repositories.GetContents(
		ctx, owner, repo, path, getOpts,
	)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return "", nil
		}

		return "", fmt.Errorf("get file contents: %w", err)
	}

	if fileContent == nil {
		return "", nil
	}

	return fileContent.GetSHA(), nil
}

func (g *gitHubProvider) createUserRepo(
	ctx context.Context,
	owner, name string,
	visibility RepoVisibility,
	repo *github.Repository,
) error {
	// Verify the authenticated user matches the requested owner.
	user, _, err := g.client.Users.Get(ctx, "")
	if err != nil {
		return fmt.Errorf("verifying authenticated user: %w", err)
	}

	if !strings.EqualFold(user.GetLogin(), owner) {
		return fmt.Errorf(
			"%w: token belongs to %q but repo requested under %q",
			ErrOwnerMismatch, user.GetLogin(), owner,
		)
	}

	// Internal visibility is not supported for user repos; fall back to private.
	if visibility == VisibilityInternal {
		repo.Private = new(true)
		repo.Visibility = nil
	}

	// Create under authenticated user (empty string = user endpoint).
	_, _, err = g.client.Repositories.Create(ctx, "", repo)
	if err != nil {
		return g.classifyCreateError(err, owner, name)
	}

	return nil
}

func (g *gitHubProvider) classifyCreateError(
	err error, owner, name string,
) error {
	errStr := err.Error()

	if strings.Contains(errStr, "name already exists") {
		return fmt.Errorf("%w: %s/%s", ErrRepoAlreadyExists, owner, name)
	}

	return fmt.Errorf(
		"%w during create repository: %w",
		ErrGitHubAPI,
		err,
	)
}

func resolveVisibility(visibility RepoVisibility) (bool, string) {
	switch visibility {
	case VisibilityPublic:
		return false, visibilityPublicStr
	case VisibilityInternal:
		return false, visibilityInternalStr
	case VisibilityPrivate:
		return true, visibilityPrivateStr
	default:
		return true, visibilityPrivateStr
	}
}

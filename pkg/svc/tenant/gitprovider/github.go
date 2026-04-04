package gitprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

const githubAPIURL = "https://api.github.com"

type gitHubProvider struct {
	token  string
	client *http.Client
	apiURL string
}

func newGitHubProvider(token string) *gitHubProvider {
	return &gitHubProvider{
		token:  token,
		client: &http.Client{},
		apiURL: githubAPIURL,
	}
}

// CreateRepo creates a GitHub repository.
// If owner is an org, creates under /orgs/{owner}/repos.
// Otherwise falls back to /user/repos.
func (g *gitHubProvider) CreateRepo(ctx context.Context, owner, name string, visibility RepoVisibility) error {
	private := visibility != VisibilityPublic
	body := map[string]any{
		"name":      name,
		"private":   private,
		"auto_init": false,
	}

	// Try org first, fall back to user
	url := fmt.Sprintf("%s/orgs/%s/repos", g.apiURL, owner)
	resp, err := g.doJSON(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("create repo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Owner is a user, not an org
		url = fmt.Sprintf("%s/user/repos", g.apiURL)
		resp2, err := g.doJSON(ctx, http.MethodPost, url, body)
		if err != nil {
			return fmt.Errorf("create user repo request: %w", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode >= 300 {
			return g.readError(resp2, "create repository")
		}
		return nil
	}

	if resp.StatusCode >= 300 {
		return g.readError(resp, "create repository")
	}
	return nil
}

// PushFiles pushes files to a repository using the Contents API.
func (g *gitHubProvider) PushFiles(ctx context.Context, owner, name string, files map[string][]byte, commitMsg string) error {
	// Sort filenames for deterministic ordering
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		content := files[path]
		encoded := base64.StdEncoding.EncodeToString(content)
		body := map[string]any{
			"message": commitMsg,
			"content": encoded,
		}
		url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", g.apiURL, owner, name, path)
		resp, err := g.doJSON(ctx, http.MethodPut, url, body)
		if err != nil {
			return fmt.Errorf("push file %s: %w", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("push file %s: HTTP %d", path, resp.StatusCode)
		}
	}
	return nil
}

// DeleteRepo deletes a GitHub repository.
func (g *gitHubProvider) DeleteRepo(ctx context.Context, owner, name string) error {
	url := fmt.Sprintf("%s/repos/%s/%s", g.apiURL, owner, name)
	resp, err := g.doRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("delete repo request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return g.readError(resp, "delete repository")
	}
	return nil
}

func (g *gitHubProvider) doJSON(ctx context.Context, method, url string, body any) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return g.doRequest(ctx, method, url, bytes.NewReader(jsonBody))
}

func (g *gitHubProvider) doRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return g.client.Do(req)
}

func (g *gitHubProvider) readError(resp *http.Response, action string) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("GitHub API error during %s (HTTP %d): %s", action, resp.StatusCode, string(body))
}

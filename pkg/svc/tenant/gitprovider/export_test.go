package gitprovider

import (
	"net/http"

	"github.com/google/go-github/v72/github"
)

// ExportNewGitHubProviderForTest creates a gitHubProvider with a custom API URL for testing.
func ExportNewGitHubProviderForTest(token string, httpClient *http.Client, apiURL string) Provider {
	client := github.NewClient(httpClient).WithAuthToken(token)
	client.UserAgent = userAgent

	// Set the base URL to the test server.
	baseURL, _ := client.BaseURL.Parse(apiURL + "/")
	client.BaseURL = baseURL
	client.UploadURL = baseURL

	return &gitHubProvider{client: client}
}

// ExportSetResolveGitHubTokenForTest replaces the resolveGitHubToken function for testing.
// Returns a restore function that should be called via defer.
func ExportSetResolveGitHubTokenForTest(fn func() string) func() {
	original := resolveGitHubToken
	resolveGitHubToken = fn

	return func() { resolveGitHubToken = original }
}

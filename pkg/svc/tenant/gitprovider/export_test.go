package gitprovider

import "net/http"

// ExportNewGitHubProviderForTest creates a gitHubProvider with custom settings for testing.
func ExportNewGitHubProviderForTest(token string, client *http.Client, apiURL string) Provider {
	return &gitHubProvider{token: token, client: client, apiURL: apiURL}
}

// ExportSetResolveGitHubTokenForTest replaces the resolveGitHubToken function for testing.
// Returns a restore function that should be called via defer.
func ExportSetResolveGitHubTokenForTest(fn func() string) func() {
	original := resolveGitHubToken
	resolveGitHubToken = fn

	return func() { resolveGitHubToken = original }
}

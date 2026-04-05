package gitprovider

import "net/http"

// ExportNewGitHubProviderForTest creates a gitHubProvider with custom settings for testing.
func ExportNewGitHubProviderForTest(token string, client *http.Client, apiURL string) Provider {
	return &gitHubProvider{token: token, client: client, apiURL: apiURL}
}

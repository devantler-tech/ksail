package gitprovider_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant/gitprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestResolveProviderHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		expected string
	}{
		{
			name:     "github maps to github.com",
			provider: "github",
			expected: "github.com",
		},
		{
			name:     "GitHub case insensitive",
			provider: "GitHub",
			expected: "github.com",
		},
		{
			name:     "GITHUB uppercase",
			provider: "GITHUB",
			expected: "github.com",
		},
		{
			name:     "gitlab maps to gitlab.com",
			provider: "gitlab",
			expected: "gitlab.com",
		},
		{
			name:     "GitLab case insensitive",
			provider: "GitLab",
			expected: "gitlab.com",
		},
		{
			name:     "gitea maps to gitea.com",
			provider: "gitea",
			expected: "gitea.com",
		},
		{
			name:     "Gitea case insensitive",
			provider: "Gitea",
			expected: "gitea.com",
		},
		{
			name:     "unknown provider returned as-is",
			provider: "custom.example.com",
			expected: "custom.example.com",
		},
		{
			name:     "bitbucket returned as-is",
			provider: "bitbucket",
			expected: "bitbucket",
		},
		{
			name:     "empty string returned as-is",
			provider: "",
			expected: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := gitprovider.ResolveProviderHost(testCase.provider)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestResolveToken_GitLabEnvVar(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "gitlab-test-token")

	got := gitprovider.ResolveToken("gitlab", "")
	require.Equal(t, "gitlab-test-token", got)
}

func TestResolveToken_GiteaEnvVar(t *testing.T) {
	t.Setenv("GITEA_TOKEN", "gitea-test-token")

	got := gitprovider.ResolveToken("gitea", "")
	require.Equal(t, "gitea-test-token", got)
}

func TestResolveToken_GitLabExplicitOverridesEnv(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "env-token")

	got := gitprovider.ResolveToken("gitlab", "explicit-token")
	require.Equal(t, "explicit-token", got)
}

func TestResolveToken_GiteaExplicitOverridesEnv(t *testing.T) {
	t.Setenv("GITEA_TOKEN", "env-token")

	got := gitprovider.ResolveToken("gitea", "explicit-token")
	require.Equal(t, "explicit-token", got)
}

func TestResolveToken_GitLabEmptyEnvVar(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")

	got := gitprovider.ResolveToken("gitlab", "")
	require.Empty(t, got)
}

func TestResolveToken_GiteaEmptyEnvVar(t *testing.T) {
	t.Setenv("GITEA_TOKEN", "")

	got := gitprovider.ResolveToken("gitea", "")
	require.Empty(t, got)
}

func TestNew_CaseInsensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
	}{
		{"GitHub mixed case", "GitHub"},
		{"GITHUB uppercase", "GITHUB"},
		{"github lowercase", "github"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provider, err := gitprovider.New(testCase.provider, "test-token")
			require.NoError(t, err)
			require.NotNil(t, provider)
		})
	}
}

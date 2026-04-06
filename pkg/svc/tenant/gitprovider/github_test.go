package gitprovider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant/gitprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveToken_ExplicitToken(t *testing.T) {
	t.Parallel()

	got := gitprovider.ResolveToken("github", "my-explicit-token")
	require.Equal(t, "my-explicit-token", got)
}

//nolint:paralleltest // mutates package-level resolveGitHubToken
func TestResolveToken_ExplicitTokenOverridesSDK(t *testing.T) {
	restore := gitprovider.ExportSetResolveGitHubTokenForTest(func() string {
		return "sdk-token-should-be-ignored"
	})
	defer restore()

	got := gitprovider.ResolveToken("github", "explicit-wins")
	require.Equal(t, "explicit-wins", got)
}

//nolint:paralleltest // mutates package-level resolveGitHubToken
func TestResolveToken_GitHubSDKFallback(t *testing.T) {
	restore := gitprovider.ExportSetResolveGitHubTokenForTest(func() string {
		return "sdk-resolved-token"
	})
	defer restore()

	got := gitprovider.ResolveToken("github", "")
	require.Equal(t, "sdk-resolved-token", got)
}

//nolint:paralleltest // mutates package-level resolveGitHubToken
func TestResolveToken_GitHubSDKReturnsEmpty(t *testing.T) {
	restore := gitprovider.ExportSetResolveGitHubTokenForTest(func() string {
		return ""
	})
	defer restore()

	got := gitprovider.ResolveToken("github", "")
	require.Empty(t, got)
}

func TestResolveToken_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	got := gitprovider.ResolveToken("bitbucket", "")
	require.Empty(t, got)
}

func TestNew_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	_, err := gitprovider.New("bitbucket", "token")
	require.ErrorIs(t, err, gitprovider.ErrUnsupportedProvider)
	require.ErrorContains(t, err, "bitbucket")
}

func TestNew_EmptyToken(t *testing.T) {
	t.Parallel()

	_, err := gitprovider.New("github", "")
	require.ErrorIs(t, err, gitprovider.ErrTokenRequired)
}

func TestNew_GitHubSuccess(t *testing.T) {
	t.Parallel()

	provider, err := gitprovider.New("github", "test-token")
	require.NoError(t, err)
	require.NotNil(t, provider)
}

func TestParseOwnerRepo_Valid(t *testing.T) {
	t.Parallel()

	owner, repo, err := gitprovider.ParseOwnerRepo("my-org/my-repo")
	require.NoError(t, err)
	require.Equal(t, "my-org", owner)
	require.Equal(t, "my-repo", repo)
}

func TestParseOwnerRepo_InvalidFormats(t *testing.T) {
	t.Parallel()

	tests := []string{
		"noslash",
		"/missing-owner",
		"missing-repo/",
		"",
	}
	for _, input := range tests {
		_, _, err := gitprovider.ParseOwnerRepo(input)
		require.Error(t, err, "expected error for input %q", input)
		require.ErrorContains(t, err, "invalid git-repo format")
	}
}

func TestGitHubProvider_CreateRepo_Org(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			assert.Equal(t, "Bearer test-token", request.Header.Get("Authorization"))

			if request.Method == http.MethodPost && request.URL.Path == "/orgs/my-org/repos" {
				var body map[string]any
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				assert.Equal(t, "my-repo", body["name"])
				assert.Equal(t, true, body["private"])
				writer.WriteHeader(http.StatusCreated)
				_, _ = writer.Write([]byte(`{"full_name":"my-org/my-repo"}`))

				return
			}

			writer.WriteHeader(http.StatusNotFound)
		}),
	)
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	err := provider.CreateRepo(
		context.Background(),
		"my-org",
		"my-repo",
		gitprovider.VisibilityPrivate,
	)
	require.NoError(t, err)
}

func TestGitHubProvider_CreateRepo_UserFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch {
			case request.Method == http.MethodPost && request.URL.Path == "/orgs/my-user/repos":
				writer.WriteHeader(http.StatusNotFound)
			case request.Method == http.MethodGet && request.URL.Path == "/user":
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(`{"login":"my-user"}`))
			case request.Method == http.MethodPost && request.URL.Path == "/user/repos":
				var body map[string]any
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				assert.Equal(t, "my-repo", body["name"])
				assert.Equal(t, false, body["private"])
				writer.WriteHeader(http.StatusCreated)
				_, _ = writer.Write([]byte(`{"full_name":"my-user/my-repo"}`))
			default:
				writer.WriteHeader(http.StatusNotFound)
			}
		}),
	)
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	err := provider.CreateRepo(
		context.Background(),
		"my-user",
		"my-repo",
		gitprovider.VisibilityPublic,
	)
	require.NoError(t, err)
}

func TestGitHubProvider_CreateRepo_Error(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = writer.Write(
			[]byte(
				`{"message":"Validation Failed","errors":[{"message":"name already exists on this account"}]}`,
			),
		)
	}))
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	err := provider.CreateRepo(
		context.Background(),
		"my-org",
		"my-repo",
		gitprovider.VisibilityPrivate,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, gitprovider.ErrRepoAlreadyExists)
}

func TestGitHubProvider_CreateRepo_422_Other(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = writer.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	err := provider.CreateRepo(
		context.Background(),
		"my-org",
		"my-repo",
		gitprovider.VisibilityPrivate,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, gitprovider.ErrGitHubAPI)
	require.NotErrorIs(t, err, gitprovider.ErrRepoAlreadyExists)
}

func TestGitHubProvider_DeleteRepo(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			assert.Equal(t, http.MethodDelete, request.Method)
			assert.Equal(t, "/repos/my-org/my-repo", request.URL.Path)
			writer.WriteHeader(http.StatusNoContent)
		}),
	)
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	err := provider.DeleteRepo(context.Background(), "my-org", "my-repo")
	require.NoError(t, err)
}

func TestGitHubProvider_DeleteRepo_Error(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"message":"Must have admin rights"}`))
	}))
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	err := provider.DeleteRepo(context.Background(), "my-org", "my-repo")
	require.Error(t, err)
	require.ErrorContains(t, err, "Must have admin rights")
}

func TestGitHubProvider_PushFiles(t *testing.T) {
	t.Parallel()

	var pushedPaths []string

	srv := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodGet {
				// File doesn't exist yet — return 404
				writer.WriteHeader(http.StatusNotFound)

				return
			}

			assert.Equal(t, http.MethodPut, request.Method)
			pushedPaths = append(pushedPaths, request.URL.Path)

			var body map[string]any
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, "initial commit", body["message"])
			assert.NotEmpty(t, body["content"])
			// No SHA expected for new files
			assert.Nil(t, body["sha"])
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"content":{"path":"test"}}`))
		}),
	)
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	files := map[string][]byte{
		"b.txt": []byte("file-b"),
		"a.txt": []byte("file-a"),
	}
	err := provider.PushFiles(context.Background(), "my-org", "my-repo", files, "initial commit")
	require.NoError(t, err)

	// Verify deterministic ordering (sorted)
	require.Len(t, pushedPaths, 2)
	require.Equal(t, "/repos/my-org/my-repo/contents/a.txt", pushedPaths[0])
	require.Equal(t, "/repos/my-org/my-repo/contents/b.txt", pushedPaths[1])
}

func TestGitHubProvider_PushFiles_WithExistingSHA(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodGet {
				// File exists — return SHA
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(`{"sha":"abc123"}`))

				return
			}

			assert.Equal(t, http.MethodPut, request.Method)

			var body map[string]any
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, "abc123", body["sha"])
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"content":{"path":"test"}}`))
		}),
	)
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	files := map[string][]byte{"file.txt": []byte("updated")}
	err := provider.PushFiles(context.Background(), "my-org", "my-repo", files, "update")
	require.NoError(t, err)
}

func TestGitHubProvider_PushFiles_Error(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodGet {
				// File doesn't exist
				writer.WriteHeader(http.StatusNotFound)

				return
			}

			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"message":"Internal error"}`))
		}),
	)
	defer srv.Close()

	provider := gitprovider.ExportNewGitHubProviderForTest("test-token", srv.Client(), srv.URL)
	files := map[string][]byte{"file.txt": []byte("content")}
	err := provider.PushFiles(context.Background(), "my-org", "my-repo", files, "commit")
	require.Error(t, err)
	require.ErrorContains(t, err, "push file file.txt")
}

func TestParseVisibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected gitprovider.RepoVisibility
		wantErr  bool
	}{
		{"Private", gitprovider.VisibilityPrivate, false},
		{"private", gitprovider.VisibilityPrivate, false},
		{"PRIVATE", gitprovider.VisibilityPrivate, false},
		{"Internal", gitprovider.VisibilityInternal, false},
		{"internal", gitprovider.VisibilityInternal, false},
		{"Public", gitprovider.VisibilityPublic, false},
		{"public", gitprovider.VisibilityPublic, false},
		{"invalid", "", true},
		{"", "", true},
	}
	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			got, err := gitprovider.ParseVisibility(testCase.input)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.expected, got)
			}
		})
	}
}

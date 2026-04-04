package gitprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveToken_ExplicitToken(t *testing.T) {
	t.Parallel()
	got := ResolveToken("github", "my-explicit-token")
	require.Equal(t, "my-explicit-token", got)
}

func TestResolveToken_EnvVarFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token-value")
	got := ResolveToken("github", "")
	require.Equal(t, "env-token-value", got)
}

func TestResolveToken_NoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	got := ResolveToken("github", "")
	require.Empty(t, got)
}

func TestResolveToken_UnsupportedProvider(t *testing.T) {
	t.Parallel()
	got := ResolveToken("bitbucket", "")
	require.Empty(t, got)
}

func TestNew_UnsupportedProvider(t *testing.T) {
	t.Parallel()
	_, err := New("bitbucket", "token")
	require.ErrorIs(t, err, ErrUnsupportedProvider)
	require.ErrorContains(t, err, "bitbucket")
}

func TestNew_EmptyToken(t *testing.T) {
	t.Parallel()
	_, err := New("github", "")
	require.ErrorIs(t, err, ErrTokenRequired)
}

func TestNew_GitHubSuccess(t *testing.T) {
	t.Parallel()
	p, err := New("github", "test-token")
	require.NoError(t, err)
	require.NotNil(t, p)
}

func TestParseOwnerRepo_Valid(t *testing.T) {
	t.Parallel()
	owner, repo, err := ParseOwnerRepo("my-org/my-repo")
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
		_, _, err := ParseOwnerRepo(input)
		require.Error(t, err, "expected error for input %q", input)
		require.ErrorContains(t, err, "invalid git-repo format")
	}
}

func TestGitHubProvider_CreateRepo_Org(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		if r.Method == http.MethodPost && r.URL.Path == "/orgs/my-org/repos" {
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "my-repo", body["name"])
			require.Equal(t, true, body["private"])
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"full_name":"my-org/my-repo"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	err := p.CreateRepo(context.Background(), "my-org", "my-repo", VisibilityPrivate)
	require.NoError(t, err)
}

func TestGitHubProvider_CreateRepo_UserFallback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/my-user/repos":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "my-repo", body["name"])
			require.Equal(t, false, body["private"])
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"full_name":"my-user/my-repo"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	err := p.CreateRepo(context.Background(), "my-user", "my-repo", VisibilityPublic)
	require.NoError(t, err)
}

func TestGitHubProvider_CreateRepo_Error(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	err := p.CreateRepo(context.Background(), "my-org", "my-repo", VisibilityPrivate)
	require.Error(t, err)
	require.ErrorContains(t, err, "Validation Failed")
}

func TestGitHubProvider_DeleteRepo(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/repos/my-org/my-repo", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	err := p.DeleteRepo(context.Background(), "my-org", "my-repo")
	require.NoError(t, err)
}

func TestGitHubProvider_DeleteRepo_Error(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Must have admin rights"}`))
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	err := p.DeleteRepo(context.Background(), "my-org", "my-repo")
	require.Error(t, err)
	require.ErrorContains(t, err, "Must have admin rights")
}

func TestGitHubProvider_PushFiles(t *testing.T) {
	t.Parallel()
	var pushedPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// File doesn't exist yet — return 404
			w.WriteHeader(http.StatusNotFound)
			return
		}
		require.Equal(t, http.MethodPut, r.Method)
		pushedPaths = append(pushedPaths, r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "initial commit", body["message"])
		require.NotEmpty(t, body["content"])
		// No SHA expected for new files
		require.Nil(t, body["sha"])
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"content":{"path":"test"}}`))
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	files := map[string][]byte{
		"b.txt": []byte("file-b"),
		"a.txt": []byte("file-a"),
	}
	err := p.PushFiles(context.Background(), "my-org", "my-repo", files, "initial commit")
	require.NoError(t, err)

	// Verify deterministic ordering (sorted)
	require.Len(t, pushedPaths, 2)
	require.Equal(t, "/repos/my-org/my-repo/contents/a.txt", pushedPaths[0])
	require.Equal(t, "/repos/my-org/my-repo/contents/b.txt", pushedPaths[1])
}

func TestGitHubProvider_PushFiles_WithExistingSHA(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// File exists — return SHA
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sha":"abc123"}`))
			return
		}
		require.Equal(t, http.MethodPut, r.Method)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "abc123", body["sha"])
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":{"path":"test"}}`))
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	files := map[string][]byte{"file.txt": []byte("updated")}
	err := p.PushFiles(context.Background(), "my-org", "my-repo", files, "update")
	require.NoError(t, err)
}

func TestGitHubProvider_PushFiles_Error(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// File doesn't exist
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"Internal error"}`))
	}))
	defer srv.Close()

	p := &gitHubProvider{token: "test-token", client: srv.Client(), apiURL: srv.URL}
	files := map[string][]byte{"file.txt": []byte("content")}
	err := p.PushFiles(context.Background(), "my-org", "my-repo", files, "commit")
	require.Error(t, err)
	require.ErrorContains(t, err, "push file file.txt")
}

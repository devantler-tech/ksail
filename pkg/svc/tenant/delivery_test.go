package tenant_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteURL_SSH(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "standard SSH",
			url:      "git@github.com:my-org/my-repo.git",
			expected: "my-org/my-repo",
		},
		{
			name:     "SSH without .git suffix",
			url:      "git@github.com:my-org/my-repo",
			expected: "my-org/my-repo",
		},
		{
			name:     "SSH with custom host",
			url:      "git@gitlab.example.com:team/project.git",
			expected: "team/project",
		},
		{
			name:     "ssh:// URL format",
			url:      "ssh://git@github.com/my-org/my-repo.git",
			expected: "my-org/my-repo",
		},
		{
			name:     "ssh:// URL without .git suffix",
			url:      "ssh://git@github.com/my-org/my-repo",
			expected: "my-org/my-repo",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := tenant.ParseRemoteURL(testCase.url)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestParseRemoteURL_HTTPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "standard HTTPS",
			url:      "https://github.com/my-org/my-repo.git",
			expected: "my-org/my-repo",
		},
		{
			name:     "HTTPS without .git suffix",
			url:      "https://github.com/my-org/my-repo",
			expected: "my-org/my-repo",
		},
		{
			name:     "HTTPS with trailing slash",
			url:      "https://github.com/my-org/my-repo/",
			expected: "my-org/my-repo",
		},
		{
			name:     "HTTP scheme",
			url:      "http://github.com/my-org/my-repo.git",
			expected: "my-org/my-repo",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := tenant.ParseRemoteURL(testCase.url)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, got)
		})
	}
}

func TestParseRemoteURL_Invalid(t *testing.T) {
	t.Parallel()

	tests := []string{
		"not-a-url",
		"ftp://example.com/repo",
		"",
	}

	for _, url := range tests {
		_, err := tenant.ParseRemoteURL(url)
		require.Error(t, err, "expected error for input %q", url)
		require.ErrorIs(t, err, tenant.ErrPlatformRepoRequired)
	}
}

func TestCollectDeliveryFiles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	tenantDir := repoRoot + "/tenants"
	require.NoError(t, os.MkdirAll(tenantDir+"/my-tenant", 0o750))
	require.NoError(
		t,
		os.WriteFile(tenantDir+"/my-tenant/namespace.yaml", []byte("kind: Namespace"), 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(tenantDir+"/my-tenant/rbac.yaml", []byte("kind: ClusterRoleBinding"), 0o600),
	)

	kustomizationPath := tenantDir + "/kustomization.yaml"
	require.NoError(t, os.WriteFile(kustomizationPath, []byte("resources:\n- my-tenant"), 0o600))

	files, err := tenant.CollectDeliveryFiles("my-tenant", tenantDir, kustomizationPath, repoRoot)
	require.NoError(t, err)

	// Should have 3 files: 2 tenant files + kustomization.yaml
	require.Len(t, files, 3)
	require.Contains(t, files, "tenants/my-tenant/namespace.yaml")
	require.Contains(t, files, "tenants/my-tenant/rbac.yaml")
	require.Contains(t, files, "tenants/kustomization.yaml")

	// Verify content
	require.Equal(t, []byte("kind: Namespace"), files["tenants/my-tenant/namespace.yaml"])
	require.Equal(t, []byte("resources:\n- my-tenant"), files["tenants/kustomization.yaml"])
}

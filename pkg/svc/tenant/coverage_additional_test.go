package tenant_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- GenerateRBACManifests error paths ---

func TestGenerateRBACManifests_EmptyNamespaces(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{},
		ClusterRole: "edit",
	}

	_, err := tenant.GenerateRBACManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrNamespaceRequired)
}

// --- GenerateFluxSyncManifests error paths ---

func TestGenerateFluxSyncManifests_EmptyNamespaces(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "test-tenant",
		Namespaces: []string{},
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		TenantRepo: "org/repo",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrNamespaceRequired)
}

func TestGenerateFluxSyncManifests_UnsupportedSyncSource(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "test-tenant",
		Namespaces: []string{"ns1"},
		SyncSource: "invalid-source",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrUnsupportedSyncSource)
}

func TestGenerateFluxSyncManifests_OCIMissingRegistry(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "test-tenant",
		Namespaces: []string{"ns1"},
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "",
		TenantRepo: "org/repo",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrRegistryRequired)
}

func TestGenerateFluxSyncManifests_OCIMissingTenantRepo(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "test-tenant",
		Namespaces: []string{"ns1"},
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		TenantRepo: "",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrTenantRepoRequired)
}

func TestGenerateFluxSyncManifests_GitMissingProvider(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{"ns1"},
		SyncSource:  tenant.SyncSourceGit,
		GitProvider: "",
		TenantRepo:  "org/repo",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrGitProviderRequired)
}

func TestGenerateFluxSyncManifests_GitMissingTenantRepo(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{"ns1"},
		SyncSource:  tenant.SyncSourceGit,
		GitProvider: "github",
		TenantRepo:  "",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrTenantRepoRequired)
}

func TestGenerateFluxSyncManifests_GitInvalidTenantRepo(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{"ns1"},
		SyncSource:  tenant.SyncSourceGit,
		GitProvider: "github",
		TenantRepo:  "invalid-no-slash",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
}

func TestGenerateFluxSyncManifests_OCIInvalidTenantRepo(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "test-tenant",
		Namespaces: []string{"ns1"},
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		TenantRepo: "no-slash",
	}

	_, err := tenant.GenerateFluxSyncManifests(opts)

	require.Error(t, err)
}

func TestGenerateFluxSyncManifests_OCIRegistryTrailingSlash(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "test-tenant",
		Namespaces: []string{"ns1"},
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io/",
		TenantRepo: "org/repo",
	}

	result, err := tenant.GenerateFluxSyncManifests(opts)

	require.NoError(t, err)

	syncYAML := result["sync.yaml"]
	// Should not contain double slashes from trailing slash
	assert.NotContains(t, syncYAML, "ghcr.io//")
}

// --- GenerateArgoCDManifests error paths ---

func TestGenerateArgoCDManifests_MissingGitProvider(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{"ns1"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "",
		TenantRepo:  "org/repo",
	}

	_, err := tenant.GenerateArgoCDManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrGitProviderRequired)
}

func TestGenerateArgoCDManifests_MissingTenantRepo(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{"ns1"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "",
	}

	_, err := tenant.GenerateArgoCDManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrTenantRepoRequired)
}

func TestGenerateArgoCDManifests_MissingNamespaces(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "org/repo",
	}

	_, err := tenant.GenerateArgoCDManifests(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrNamespaceRequired)
}

func TestGenerateArgoCDManifests_GiteaProvider(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "test-tenant",
		Namespaces:  []string{"ns1"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "gitea",
		TenantRepo:  "org/repo",
	}

	result, err := tenant.GenerateArgoCDManifests(opts)

	require.NoError(t, err)
	require.Contains(t, result, "project.yaml")
	require.Contains(t, result, "app.yaml")
}

func TestGenerateArgoCDManifests_ProjectHasAllDestinations(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "multi-dest",
		Namespaces:  []string{"ns1", "ns2", "ns3"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "org/repo",
	}

	result, err := tenant.GenerateArgoCDManifests(opts)

	require.NoError(t, err)

	projectYAML := result["project.yaml"]

	// All namespaces should appear as destinations
	for _, ns := range opts.Namespaces {
		assert.Contains(t, projectYAML, ns)
	}
}

// --- ScaffoldFiles tests ---

func TestScaffoldFiles_Flux(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "my-tenant",
		TenantType: tenant.TypeFlux,
	}

	files := tenant.ScaffoldFiles(opts)

	require.Contains(t, files, "README.md")
	require.Contains(t, files, "k8s/kustomization.yaml")

	readmeContent := string(files["README.md"])
	assert.Contains(t, readmeContent, "my-tenant")
	assert.Contains(t, readmeContent, "Flux")
}

func TestScaffoldFiles_ArgoCD(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "my-tenant",
		TenantType: tenant.TypeArgoCD,
	}

	files := tenant.ScaffoldFiles(opts)

	require.Contains(t, files, "README.md")
	require.Contains(t, files, "k8s/kustomization.yaml")

	readmeContent := string(files["README.md"])
	assert.Contains(t, readmeContent, "my-tenant")
	assert.Contains(t, readmeContent, "ArgoCD")
}

func TestScaffoldFiles_Kubectl(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "my-tenant",
		TenantType: tenant.TypeKubectl,
	}

	files := tenant.ScaffoldFiles(opts)

	require.Contains(t, files, "README.md")
	require.Contains(t, files, "k8s/kustomization.yaml")

	readmeContent := string(files["README.md"])
	assert.Contains(t, readmeContent, "kubectl apply")
}

func TestScaffoldFiles_DefaultType(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "my-tenant",
		TenantType: "",
	}

	files := tenant.ScaffoldFiles(opts)

	require.Contains(t, files, "README.md")
	require.Contains(t, files, "k8s/kustomization.yaml")

	readmeContent := string(files["README.md"])
	assert.Contains(t, readmeContent, "KSail-managed tenant")
}

func TestScaffoldFiles_KustomizationContent(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "test",
		TenantType: tenant.TypeFlux,
	}

	files := tenant.ScaffoldFiles(opts)

	kContent := string(files["k8s/kustomization.yaml"])
	assert.Contains(t, kContent, "apiVersion: kustomize.config.k8s.io/v1beta1")
	assert.Contains(t, kContent, "kind: Kustomization")
	assert.Contains(t, kContent, "resources: []")
}

// --- Options validation tests ---

func TestOptions_ResolveDefaults(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name: "my-tenant",
	}

	opts.ResolveDefaults()

	assert.Equal(t, []string{"my-tenant"}, opts.Namespaces,
		"namespaces should default to tenant name")
	assert.Equal(t, tenant.DefaultClusterRole, opts.ClusterRole)
	assert.Equal(t, tenant.DefaultOutputDir, opts.OutputDir)
	assert.Equal(t, tenant.DefaultSyncSource, opts.SyncSource)
	assert.Equal(t, tenant.DefaultRepoVisibility, opts.RepoVisibility)
}

func TestOptions_ResolveDefaults_PreservesExisting(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:           "my-tenant",
		Namespaces:     []string{"ns1", "ns2"},
		ClusterRole:    "admin",
		OutputDir:      "/custom/path",
		SyncSource:     tenant.SyncSourceGit,
		RepoVisibility: "Public",
	}

	opts.ResolveDefaults()

	assert.Equal(t, []string{"ns1", "ns2"}, opts.Namespaces)
	assert.Equal(t, "admin", opts.ClusterRole)
	assert.Equal(t, "/custom/path", opts.OutputDir)
	assert.Equal(t, tenant.SyncSourceGit, opts.SyncSource)
	assert.Equal(t, "Public", opts.RepoVisibility)
}

//nolint:funlen // Table-driven test coverage is naturally long.
func TestOptions_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    tenant.Options
		wantErr error
	}{
		{
			name:    "empty name",
			opts:    tenant.Options{TenantType: tenant.TypeFlux},
			wantErr: tenant.ErrTenantNameRequired,
		},
		{
			name: "invalid name - too long",
			opts: tenant.Options{
				Name:       strings.Repeat("a", 64),
				TenantType: tenant.TypeFlux,
			},
			wantErr: tenant.ErrInvalidTenantName,
		},
		{
			name: "invalid name - uppercase",
			opts: tenant.Options{
				Name:       "MyTenant",
				TenantType: tenant.TypeFlux,
			},
			wantErr: tenant.ErrInvalidTenantName,
		},
		{
			name: "invalid name - path separator",
			opts: tenant.Options{
				Name:       "my/tenant",
				TenantType: tenant.TypeFlux,
			},
			wantErr: tenant.ErrInvalidTenantName,
		},
		{
			name: "invalid name - double dots",
			opts: tenant.Options{
				Name:       "my..tenant",
				TenantType: tenant.TypeFlux,
			},
			wantErr: tenant.ErrInvalidTenantName,
		},
		{
			name: "missing tenant type",
			opts: tenant.Options{
				Name: "my-tenant",
			},
			wantErr: tenant.ErrTenantTypeRequired,
		},
		{
			name: "invalid tenant type",
			opts: tenant.Options{
				Name:       "my-tenant",
				TenantType: "invalid",
			},
			wantErr: tenant.ErrInvalidType,
		},
		{
			name: "invalid namespace",
			opts: tenant.Options{
				Name:       "my-tenant",
				TenantType: tenant.TypeFlux,
				Namespaces: []string{"Invalid_NS"},
			},
			wantErr: tenant.ErrInvalidNamespace,
		},
		{
			name: "duplicate namespaces",
			opts: tenant.Options{
				Name:       "my-tenant",
				TenantType: tenant.TypeFlux,
				Namespaces: []string{"ns1", "ns1"},
			},
			wantErr: tenant.ErrDuplicateNamespace,
		},
		{
			name: "valid options",
			opts: tenant.Options{
				Name:       "my-tenant",
				TenantType: tenant.TypeFlux,
				Namespaces: []string{"ns1", "ns2"},
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.opts.Validate()

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// --- Type tests ---

func TestType_Set(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    tenant.Type
		wantErr bool
	}{
		{"flux lowercase", "flux", tenant.TypeFlux, false},
		{"flux uppercase", "FLUX", tenant.TypeFlux, false},
		{"flux mixed case", "Flux", tenant.TypeFlux, false},
		{"argocd lowercase", "argocd", tenant.TypeArgoCD, false},
		{"argocd mixed", "ArgoCD", tenant.TypeArgoCD, false},
		{"kubectl", "kubectl", tenant.TypeKubectl, false},
		{"invalid", "invalid", "", true},
		{"empty", "", "", true},
	}

	//nolint:varnamelen // Short names keep table-driven tests readable.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var typ tenant.Type

			err := typ.Set(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, tenant.ErrInvalidType)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, typ)
			}
		})
	}
}

func TestType_String(t *testing.T) {
	t.Parallel()

	flux := tenant.TypeFlux
	assert.Equal(t, "flux", flux.String())

	argocd := tenant.TypeArgoCD
	assert.Equal(t, "argocd", argocd.String())

	kubectl := tenant.TypeKubectl
	assert.Equal(t, "kubectl", kubectl.String())

	// Test nil case via pointer
	var nilType *tenant.Type
	assert.Empty(t, nilType.String())
}

func TestType_TypeMethodReturnsTypeName(t *testing.T) {
	t.Parallel()

	typ := tenant.TypeFlux
	assert.Equal(t, "TenantType", typ.Type())
}

func TestValidTypes_Complete(t *testing.T) {
	t.Parallel()

	types := tenant.ValidTypes()

	assert.Len(t, types, 3)
	assert.Contains(t, types, tenant.TypeFlux)
	assert.Contains(t, types, tenant.TypeArgoCD)
	assert.Contains(t, types, tenant.TypeKubectl)
}

// --- ManagedByLabels tests ---

func TestManagedByLabels_Content(t *testing.T) {
	t.Parallel()

	labels := tenant.ManagedByLabels()

	assert.Equal(t, "ksail", labels["app.kubernetes.io/managed-by"])
	assert.Len(t, labels, 1)
}

// --- ParseRemoteURL tests ---

//nolint:funlen // Table-driven test coverage is naturally long.
func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "https github URL",
			url:  "https://github.com/owner/repo.git",
			want: "owner/repo",
		},
		{
			name: "https github URL without .git",
			url:  "https://github.com/owner/repo",
			want: "owner/repo",
		},
		{
			name: "ssh scp-style URL",
			url:  "git@github.com:owner/repo.git",
			want: "owner/repo",
		},
		{
			name: "ssh scp-style URL without .git",
			url:  "git@github.com:owner/repo",
			want: "owner/repo",
		},
		{
			name: "ssh URL format",
			url:  "ssh://git@github.com/owner/repo.git",
			want: "owner/repo",
		},
		{
			name: "ssh URL format without .git",
			url:  "ssh://git@github.com/owner/repo",
			want: "owner/repo",
		},
		{
			name: "https gitlab URL",
			url:  "https://gitlab.com/group/project.git",
			want: "group/project",
		},
		{
			name: "http URL",
			url:  "http://gitea.local/owner/repo.git",
			want: "owner/repo",
		},
		{
			name:    "unrecognized format",
			url:     "not-a-valid-url",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := tenant.ParseRemoteURL(testCase.url)

			if testCase.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, tenant.ErrPlatformRepoRequired)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.want, got)
			}
		})
	}
}

// --- Export tests for internal functions ---

func TestExportHasDuplicateNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		namespaces []string
		want       bool
	}{
		{"no duplicates", []string{"a", "b", "c"}, false},
		{"with duplicates", []string{"a", "b", "a"}, true},
		{"empty", []string{}, false},
		{"single element", []string{"a"}, false},
		{"all same", []string{"x", "x", "x"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tenant.ExportHasDuplicateNamespaces(tt.namespaces)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestExportIsValidType(t *testing.T) {
	t.Parallel()

	assert.True(t, tenant.ExportIsValidType(tenant.TypeFlux))
	assert.True(t, tenant.ExportIsValidType(tenant.TypeArgoCD))
	assert.True(t, tenant.ExportIsValidType(tenant.TypeKubectl))
	assert.False(t, tenant.ExportIsValidType("invalid"))
	assert.False(t, tenant.ExportIsValidType(""))
}

func TestExportSafeRelPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    string
		target  string
		want    string
		wantErr bool
	}{
		{
			name:   "within base",
			base:   "/repo",
			target: "/repo/tenants/my-tenant/ns.yaml",
			want:   "tenants/my-tenant/ns.yaml",
		},
		{
			name:    "outside base",
			base:    "/repo",
			target:  "/other/file.yaml",
			wantErr: true,
		},
		{
			name:   "same as base",
			base:   "/repo",
			target: "/repo",
			want:   ".",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := tenant.ExportSafeRelPath(testCase.base, testCase.target)

			if testCase.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, tenant.ErrOutsideRepoRoot)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.want, got)
			}
		})
	}
}

// --- Generate (integration) tests ---

func TestGenerate_FluxOCI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	opts := tenant.Options{
		Name:       "my-tenant",
		Namespaces: []string{"my-tenant"},
		TenantType: tenant.TypeFlux,
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		TenantRepo: "org/repo",
		OutputDir:  dir,
		Force:      false,
	}

	err := tenant.Generate(opts)
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "my-tenant")

	// Should create expected files
	expectedFiles := []string{
		"namespace.yaml",
		"serviceaccount.yaml",
		"rolebinding.yaml",
		"sync.yaml",
		"kustomization.yaml",
	}

	for _, filename := range expectedFiles {
		filePath := filepath.Join(tenantDir, filename)
		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr, "expected file %s to exist", filename)
	}
}

func TestGenerate_ArgoCD(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	opts := tenant.Options{
		Name:        "my-tenant",
		Namespaces:  []string{"my-tenant"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "org/repo",
		OutputDir:   dir,
		Force:       false,
	}

	err := tenant.Generate(opts)
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "my-tenant")

	expectedFiles := []string{
		"namespace.yaml",
		"serviceaccount.yaml",
		"rolebinding.yaml",
		"project.yaml",
		"app.yaml",
		"kustomization.yaml",
	}

	for _, filename := range expectedFiles {
		filePath := filepath.Join(tenantDir, filename)
		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr, "expected file %s to exist", filename)
	}
}

func TestGenerate_Kubectl(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	opts := tenant.Options{
		Name:       "my-tenant",
		Namespaces: []string{"my-tenant"},
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
		Force:      false,
	}

	err := tenant.Generate(opts)
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "my-tenant")

	// Kubectl generates only RBAC + kustomization
	expectedFiles := []string{
		"namespace.yaml",
		"serviceaccount.yaml",
		"rolebinding.yaml",
		"kustomization.yaml",
	}

	for _, filename := range expectedFiles {
		filePath := filepath.Join(tenantDir, filename)
		_, statErr := os.Stat(filePath)
		require.NoError(t, statErr, "expected file %s to exist", filename)
	}

	// Should NOT have sync.yaml or ArgoCD files
	_, err = os.Stat(filepath.Join(tenantDir, "sync.yaml"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(tenantDir, "project.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestGenerate_ForceOverwriteRemovesOldFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tenantDir := filepath.Join(dir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	// Write a dummy file to verify it gets overwritten
	require.NoError(t, os.WriteFile(
		filepath.Join(tenantDir, "old-file.yaml"),
		[]byte("old content"),
		0o600,
	))

	opts := tenant.Options{
		Name:       "my-tenant",
		Namespaces: []string{"my-tenant"},
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
		Force:      true,
	}

	err := tenant.Generate(opts)
	require.NoError(t, err)

	// Old file should be removed
	_, err = os.Stat(filepath.Join(tenantDir, "old-file.yaml"))
	assert.True(t, os.IsNotExist(err))
}

func TestGenerate_ExistingDirNoForce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tenantDir := filepath.Join(dir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	opts := tenant.Options{
		Name:       "my-tenant",
		Namespaces: []string{"my-tenant"},
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
		Force:      false,
	}

	err := tenant.Generate(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrTenantAlreadyExists)
}

func TestGenerate_InvalidOptions(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name: "", // Invalid - empty name
	}

	err := tenant.Generate(opts)

	require.Error(t, err)
}

func TestGenerate_InvalidType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	opts := tenant.Options{
		Name:       "my-tenant",
		Namespaces: []string{"my-tenant"},
		TenantType: "invalid-type",
		OutputDir:  dir,
	}

	err := tenant.Generate(opts)

	require.Error(t, err)
}

// --- MergeArgoCDRBACPolicy additional edge cases ---

func TestMergeArgoCDRBACPolicy_WhitespaceOnlyExisting(t *testing.T) {
	t.Parallel()

	result, err := tenant.MergeArgoCDRBACPolicy("   \n\t\n  ", "team-alpha")

	require.NoError(t, err)
	assert.Contains(t, result, "role:team-alpha")
	assert.Contains(t, result, "argocd-rbac-cm")
}

func TestRemoveArgoCDRBACPolicy_EmptyPolicyCSV(t *testing.T) {
	t.Parallel()

	existing := `apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: ""
`

	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team-alpha")

	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestRemoveArgoCDRBACPolicy_NoPolicyCSVField(t *testing.T) {
	t.Parallel()

	existing := `apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  other-field: something
`

	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team-alpha")

	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

// --- prepareTenantDir edge cases ---

func TestGenerate_ExistingFileNotDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a file where the tenant directory would be
	filePath := filepath.Join(dir, "my-tenant")
	require.NoError(t, os.WriteFile(filePath, []byte("not a directory"), 0o600))

	opts := tenant.Options{
		Name:       "my-tenant",
		Namespaces: []string{"my-tenant"},
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
		Force:      true,
	}

	err := tenant.Generate(opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, tenant.ErrTenantAlreadyExists)
}

// --- CollectDeliveryFiles tests ---

func TestCollectDeliveryFiles_WithKustomizationIncluded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tenantName := "my-tenant"
	tenantDir := filepath.Join(dir, tenantName)
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	// Write a manifest file
	require.NoError(t, os.WriteFile(
		filepath.Join(tenantDir, "namespace.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace"),
		0o600,
	))

	// Write a kustomization file
	kustomizationPath := filepath.Join(dir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(
		kustomizationPath,
		[]byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []"),
		0o600,
	))

	files, err := tenant.CollectDeliveryFiles(tenantName, dir, kustomizationPath, dir)

	require.NoError(t, err)
	require.NotEmpty(t, files)

	// Should contain the kustomization file
	found := false

	for key := range files {
		if strings.Contains(key, "kustomization.yaml") {
			found = true

			break
		}
	}

	assert.True(t, found, "should find kustomization.yaml in collected files")
}

// --- RemoveArgoCDRBACPolicy edge case: no data field at all ---

func TestRemoveArgoCDRBACPolicy_NoDataField(t *testing.T) {
	t.Parallel()

	existing := `apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
`

	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team-alpha")

	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

// --- MergeArgoCDRBACPolicy invalid YAML ---

func TestMergeArgoCDRBACPolicy_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := tenant.MergeArgoCDRBACPolicy("not: valid: yaml: {{{", "team-alpha")

	require.Error(t, err)
}

// --- RemoveArgoCDRBACPolicy invalid YAML ---

func TestRemoveArgoCDRBACPolicy_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := tenant.RemoveArgoCDRBACPolicy("not: valid: yaml: {{{", "team-alpha")

	require.Error(t, err)
}

// --- Generate with Flux Git sync source ---

//nolint:gosec // Test-only fixtures use controlled temp paths and permissions.
func TestGenerate_FluxGit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	opts := tenant.Options{
		Name:        "my-tenant",
		Namespaces:  []string{"my-tenant"},
		TenantType:  tenant.TypeFlux,
		SyncSource:  tenant.SyncSourceGit,
		GitProvider: "github",
		TenantRepo:  "org/repo",
		OutputDir:   dir,
		Force:       false,
	}

	err := tenant.Generate(opts)
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "my-tenant")

	// Should have sync.yaml
	syncContent, err := os.ReadFile(filepath.Join(tenantDir, "sync.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(syncContent), "GitRepository")
}

// --- FindArgoCDRBACCM with doc starting with --- ---

func TestFindArgoCDRBACCM_DocStartingWithSeparator(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Document starts with ---
	content := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: ""
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rbac.yaml"), []byte(content), 0o600))

	found, err := tenant.FindArgoCDRBACCM(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, found)
}

// --- ExportGetResources ---

func TestExportGetResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  map[string]any
		want []string
	}{
		{
			name: "with resources",
			raw: map[string]any{
				"resources": []any{"file1.yaml", "file2.yaml"},
			},
			want: []string{"file1.yaml", "file2.yaml"},
		},
		{
			name: "no resources field",
			raw:  map[string]any{},
			want: nil,
		},
		{
			name: "empty resources",
			raw: map[string]any{
				"resources": []any{},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tenant.ExportGetResources(tt.raw)
			assert.Equal(t, tt.want, result)
		})
	}
}

// --- ExportAddResource and ExportRemoveResource ---

func TestExportAddResource_New(t *testing.T) {
	t.Parallel()

	resources := []string{"file1.yaml"}

	result := tenant.ExportAddResource(resources, "file2.yaml")

	assert.Contains(t, result, "file1.yaml")
	assert.Contains(t, result, "file2.yaml")
}

func TestExportAddResource_Duplicate(t *testing.T) {
	t.Parallel()

	resources := []string{"file1.yaml"}

	result := tenant.ExportAddResource(resources, "file1.yaml")

	assert.Equal(t, []string{"file1.yaml"}, result)
}

func TestExportRemoveResource_Existing(t *testing.T) {
	t.Parallel()

	resources := []string{"file1.yaml", "file2.yaml"}

	result := tenant.ExportRemoveResource(resources, "file1.yaml")

	assert.NotContains(t, result, "file1.yaml")
	assert.Contains(t, result, "file2.yaml")
}

func TestExportRemoveResource_NotPresent(t *testing.T) {
	t.Parallel()

	resources := []string{"file1.yaml"}

	result := tenant.ExportRemoveResource(resources, "file3.yaml")

	assert.Equal(t, []string{"file1.yaml"}, result)
}

package tenant_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/stretchr/testify/require"
)

func TestTypeSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected tenant.Type
	}{
		{"flux lowercase", "flux", tenant.TypeFlux},
		{"flux uppercase", "FLUX", tenant.TypeFlux},
		{"flux mixed case", "Flux", tenant.TypeFlux},
		{"argocd lowercase", "argocd", tenant.TypeArgoCD},
		{"argocd uppercase", "ARGOCD", tenant.TypeArgoCD},
		{"argocd mixed case", "ArgoCD", tenant.TypeArgoCD},
		{"kubectl lowercase", "kubectl", tenant.TypeKubectl},
		{"kubectl uppercase", "KUBECTL", tenant.TypeKubectl},
		{"kubectl mixed case", "Kubectl", tenant.TypeKubectl},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var tenantType tenant.Type

			err := tenantType.Set(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, tenantType)
		})
	}
}

func TestTypeSetInvalid(t *testing.T) {
	t.Parallel()

	var tenantType tenant.Type

	err := tenantType.Set("invalid")
	require.Error(t, err)
	require.ErrorIs(t, err, tenant.ErrInvalidType)
}

func TestValidTypes(t *testing.T) {
	t.Parallel()

	types := tenant.ValidTypes()
	require.Len(t, types, 3)
	require.Contains(t, types, tenant.TypeFlux)
	require.Contains(t, types, tenant.TypeArgoCD)
	require.Contains(t, types, tenant.TypeKubectl)
}

func TestOptionsResolveDefaults(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{Name: "team-alpha"}
	opts.ResolveDefaults()

	require.Equal(t, []string{"team-alpha"}, opts.Namespaces)
	require.Equal(t, tenant.DefaultClusterRole, opts.ClusterRole)
	require.Equal(t, tenant.DefaultOutputDir, opts.OutputDir)
	require.Equal(t, tenant.DefaultSyncSource, opts.SyncSource)
	require.Equal(t, tenant.DefaultRepoVisibility, opts.RepoVisibility)
}

func TestOptionsResolveDefaultsPreservesExisting(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:           "team-beta",
		Namespaces:     []string{"ns1", "ns2"},
		ClusterRole:    "admin",
		OutputDir:      "/custom",
		SyncSource:     tenant.SyncSourceGit,
		RepoVisibility: "Public",
	}
	opts.ResolveDefaults()

	require.Equal(t, []string{"ns1", "ns2"}, opts.Namespaces)
	require.Equal(t, "admin", opts.ClusterRole)
	require.Equal(t, "/custom", opts.OutputDir)
	require.Equal(t, tenant.SyncSourceGit, opts.SyncSource)
	require.Equal(t, "Public", opts.RepoVisibility)
}

func TestOptionsValidateEmptyName(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{TenantType: tenant.TypeFlux}
	err := opts.Validate()
	require.ErrorIs(t, err, tenant.ErrTenantNameRequired)
}

func TestOptionsValidateEmptyType(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{Name: "team-alpha"}
	err := opts.Validate()
	require.ErrorIs(t, err, tenant.ErrTenantTypeRequired)
}

func TestOptionsValidateSuccess(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "team-alpha",
		TenantType: tenant.TypeFlux,
	}
	err := opts.Validate()
	require.NoError(t, err)
}

func TestOptionsValidateInvalidDNSName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  string
	}{
		{"uppercase", "Team-Alpha"},
		{"underscores", "team_alpha"},
		{"starts with hyphen", "-team"},
		{"ends with hyphen", "team-"},
		{"too long", strings.Repeat("a", 64)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := tenant.Options{
				Name:       testCase.val,
				TenantType: tenant.TypeFlux,
			}
			opts.Namespaces = []string{testCase.val}
			err := opts.Validate()
			require.Error(t, err)
		})
	}
}

func TestOptionsValidatePathTraversalName(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "../escape",
		TenantType: tenant.TypeFlux,
	}
	err := opts.Validate()
	require.ErrorIs(t, err, tenant.ErrInvalidTenantName)
}

func TestOptionsValidateInvalidNamespace(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:       "valid-name",
		TenantType: tenant.TypeFlux,
		Namespaces: []string{"INVALID_NS"},
	}
	err := opts.Validate()
	require.ErrorIs(t, err, tenant.ErrInvalidNamespace)
}

func TestManagedByLabels(t *testing.T) {
	t.Parallel()

	labels := tenant.ManagedByLabels()
	require.Equal(t, map[string]string{
		"app.kubernetes.io/managed-by": "ksail",
	}, labels)
}

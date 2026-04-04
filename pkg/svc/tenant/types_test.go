package tenant

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantTypeSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected TenantType
	}{
		{"flux lowercase", "flux", TenantTypeFlux},
		{"flux uppercase", "FLUX", TenantTypeFlux},
		{"flux mixed case", "Flux", TenantTypeFlux},
		{"argocd lowercase", "argocd", TenantTypeArgoCD},
		{"argocd uppercase", "ARGOCD", TenantTypeArgoCD},
		{"argocd mixed case", "ArgoCD", TenantTypeArgoCD},
		{"kubectl lowercase", "kubectl", TenantTypeKubectl},
		{"kubectl uppercase", "KUBECTL", TenantTypeKubectl},
		{"kubectl mixed case", "Kubectl", TenantTypeKubectl},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tenantType TenantType
			err := tenantType.Set(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, tenantType)
		})
	}
}

func TestTenantTypeSetInvalid(t *testing.T) {
	t.Parallel()
	var tenantType TenantType
	err := tenantType.Set("invalid")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidTenantType))
}

func TestValidTenantTypes(t *testing.T) {
	t.Parallel()
	types := ValidTenantTypes()
	require.Len(t, types, 3)
	require.Contains(t, types, TenantTypeFlux)
	require.Contains(t, types, TenantTypeArgoCD)
	require.Contains(t, types, TenantTypeKubectl)
}

func TestOptionsResolveDefaults(t *testing.T) {
	t.Parallel()
	opts := Options{Name: "team-alpha"}
	opts.ResolveDefaults()

	require.Equal(t, []string{"team-alpha"}, opts.Namespaces)
	require.Equal(t, DefaultClusterRole, opts.ClusterRole)
	require.Equal(t, DefaultOutputDir, opts.OutputDir)
	require.Equal(t, DefaultSyncSource, opts.SyncSource)
	require.Equal(t, DefaultRepoVisibility, opts.RepoVisibility)
}

func TestOptionsResolveDefaultsPreservesExisting(t *testing.T) {
	t.Parallel()
	opts := Options{
		Name:           "team-beta",
		Namespaces:     []string{"ns1", "ns2"},
		ClusterRole:    "admin",
		OutputDir:      "/custom",
		SyncSource:     SyncSourceGit,
		RepoVisibility: "Public",
	}
	opts.ResolveDefaults()

	require.Equal(t, []string{"ns1", "ns2"}, opts.Namespaces)
	require.Equal(t, "admin", opts.ClusterRole)
	require.Equal(t, "/custom", opts.OutputDir)
	require.Equal(t, SyncSourceGit, opts.SyncSource)
	require.Equal(t, "Public", opts.RepoVisibility)
}

func TestOptionsValidateEmptyName(t *testing.T) {
	t.Parallel()
	opts := Options{TenantType: TenantTypeFlux}
	err := opts.Validate()
	require.ErrorIs(t, err, ErrTenantNameRequired)
}

func TestOptionsValidateEmptyType(t *testing.T) {
	t.Parallel()
	opts := Options{Name: "team-alpha"}
	err := opts.Validate()
	require.ErrorIs(t, err, ErrTenantTypeRequired)
}

func TestOptionsValidateSuccess(t *testing.T) {
	t.Parallel()
	opts := Options{Name: "team-alpha", TenantType: TenantTypeFlux}
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
		{"too long", "a" + string(make([]byte, 63))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := Options{Name: tt.val, TenantType: TenantTypeFlux}
			opts.Namespaces = []string{tt.val}
			err := opts.Validate()
			require.Error(t, err)
		})
	}
}

func TestOptionsValidatePathTraversalName(t *testing.T) {
	t.Parallel()
	opts := Options{Name: "../escape", TenantType: TenantTypeFlux}
	err := opts.Validate()
	require.ErrorIs(t, err, ErrInvalidTenantName)
}

func TestOptionsValidateInvalidNamespace(t *testing.T) {
	t.Parallel()
	opts := Options{Name: "valid-name", TenantType: TenantTypeFlux, Namespaces: []string{"INVALID_NS"}}
	err := opts.Validate()
	require.ErrorIs(t, err, ErrInvalidNamespace)
}

func TestManagedByLabels(t *testing.T) {
	t.Parallel()
	labels := ManagedByLabels()
	require.Equal(t, map[string]string{
		"app.kubernetes.io/managed-by": "ksail",
	}, labels)
}

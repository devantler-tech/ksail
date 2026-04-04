package tenant

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected Type
	}{
		{"flux lowercase", "flux", TypeFlux},
		{"flux uppercase", "FLUX", TypeFlux},
		{"flux mixed case", "Flux", TypeFlux},
		{"argocd lowercase", "argocd", TypeArgoCD},
		{"argocd uppercase", "ARGOCD", TypeArgoCD},
		{"argocd mixed case", "ArgoCD", TypeArgoCD},
		{"kubectl lowercase", "kubectl", TypeKubectl},
		{"kubectl uppercase", "KUBECTL", TypeKubectl},
		{"kubectl mixed case", "Kubectl", TypeKubectl},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var tenantType Type
			err := tenantType.Set(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, tenantType)
		})
	}
}

func TestTypeSetInvalid(t *testing.T) {
	t.Parallel()
	var tenantType Type
	err := tenantType.Set("invalid")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidType))
}

func TestValidTypes(t *testing.T) {
	t.Parallel()
	types := ValidTypes()
	require.Len(t, types, 3)
	require.Contains(t, types, TypeFlux)
	require.Contains(t, types, TypeArgoCD)
	require.Contains(t, types, TypeKubectl)
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
	opts := Options{TenantType: TypeFlux}
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
	opts := Options{Name: "team-alpha", TenantType: TypeFlux}
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
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			opts := Options{Name: testCase.val, TenantType: TypeFlux}
			opts.Namespaces = []string{testCase.val}
			err := opts.Validate()
			require.Error(t, err)
		})
	}
}

func TestOptionsValidatePathTraversalName(t *testing.T) {
	t.Parallel()
	opts := Options{Name: "../escape", TenantType: TypeFlux}
	err := opts.Validate()
	require.ErrorIs(t, err, ErrInvalidTenantName)
}

func TestOptionsValidateInvalidNamespace(t *testing.T) {
	t.Parallel()
	opts := Options{Name: "valid-name", TenantType: TypeFlux, Namespaces: []string{"INVALID_NS"}}
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

package tenant_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func readTenantFile(t *testing.T, dir, tenantName, filename string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, tenantName, filename)) //nolint:gosec // test path
	require.NoError(t, err)

	return string(data)
}

func TestGenerateRBACManifests_PodSecurityLabels(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateRBACManifests(tenant.Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		ClusterRole: "edit",
		PodSecurity: tenant.PodSecurityRestricted,
	})
	require.NoError(t, err)

	ns := result["namespace.yaml"]
	require.Contains(t, ns, "pod-security.kubernetes.io/enforce: restricted")
	require.Contains(t, ns, "pod-security.kubernetes.io/audit: restricted")
	require.Contains(t, ns, "pod-security.kubernetes.io/warn: restricted")
	snaps.MatchSnapshot(t, ns)
}

func TestGenerateRBACManifests_ServiceAccountHardening(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateRBACManifests(tenant.Options{
		Name:                  "team-alpha",
		Namespaces:            []string{"team-alpha"},
		ClusterRole:           "edit",
		DisableTokenAutomount: true,
		ImagePullSecrets:      []string{"ghcr-auth", "dockerhub"},
	})
	require.NoError(t, err)

	sa := result["serviceaccount.yaml"]
	require.Contains(t, sa, "automountServiceAccountToken: false")
	require.Contains(t, sa, "name: ghcr-auth")
	require.Contains(t, sa, "name: dockerhub")
	snaps.MatchSnapshot(t, sa)
}

func TestGenerateRBACManifests_MultipleClusterRoles(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateRBACManifests(tenant.Options{
		Name:         "team-alpha",
		Namespaces:   []string{"team-alpha"},
		ClusterRoles: []string{"edit", "view"},
	})
	require.NoError(t, err)

	roleBinding := result["rolebinding.yaml"]
	// One RoleBinding per role, named <tenant>-<role>.
	require.Contains(t, roleBinding, "name: team-alpha-edit")
	require.Contains(t, roleBinding, "name: team-alpha-view")
	// Both bind the single tenant ServiceAccount.
	require.Equal(t, 2, strings.Count(roleBinding, "name: team-alpha\n  namespace: team-alpha"))

	docs := strings.Split(roleBinding, "---\n")
	require.Len(t, docs, 2)
	snaps.MatchSnapshot(t, roleBinding)
}

func TestGenerateRBACManifests_ClusterRoleWithColonSanitized(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateRBACManifests(tenant.Options{
		Name:         "team-alpha",
		Namespaces:   []string{"team-alpha"},
		ClusterRoles: []string{"edit", "system:auth-delegator"},
	})
	require.NoError(t, err)

	roleBinding := result["rolebinding.yaml"]
	// roleRef keeps the canonical ClusterRole name (':' is valid there)...
	require.Contains(t, roleBinding, "name: system:auth-delegator")
	// ...but the binding metadata.name is sanitized to a valid DNS-1123 label.
	require.Contains(t, roleBinding, "name: team-alpha-system-auth-delegator")
	require.NotContains(t, roleBinding, "name: team-alpha-system:auth-delegator")
}

func TestGenerateRBACManifests_ClusterRolesTrimmedAndDeduped(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateRBACManifests(tenant.Options{
		Name:         "team-alpha",
		Namespaces:   []string{"team-alpha"},
		ClusterRoles: []string{" edit ", "edit", "view"},
	})
	require.NoError(t, err)

	roleBinding := result["rolebinding.yaml"]
	// " edit " and "edit" collapse to a single binding; no leading/trailing space.
	require.NotContains(t, roleBinding, "name:  edit")
	require.Equal(t, 1, strings.Count(roleBinding, "name: edit\n"))

	docs := strings.Split(roleBinding, "---\n")
	require.Len(t, docs, 2) // edit + view
}

func TestGenerate_ProductionTenant(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := tenant.Generate(tenant.Options{
		Name:              "team-prod",
		Namespaces:        []string{"team-prod"},
		TenantType:        tenant.TypeKubectl,
		OutputDir:         dir,
		PodSecurity:       tenant.PodSecurityBaseline,
		WithNetworkPolicy: true,
		WithQuota:         true,
		WithLimitRange:    true,
	})
	require.NoError(t, err)

	kustomization := readTenantFile(t, dir, "team-prod", "kustomization.yaml")
	for _, name := range []string{
		"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml",
		"networkpolicy.yaml", "resourcequota.yaml", "limitrange.yaml",
	} {
		require.Contains(t, kustomization, name)
	}

	snaps.MatchSnapshot(t, kustomization)
}

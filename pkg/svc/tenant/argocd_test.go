package tenant_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

const rbacCMHeader = "apiVersion: v1"

func TestGenerateArgoCDManifests_ProjectSingleNamespace(t *testing.T) {
	t.Parallel()
	opts := tenant.Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-alpha-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["project.yaml"])
}

func TestGenerateArgoCDManifests_ProjectMultipleNamespaces(t *testing.T) {
	t.Parallel()
	opts := tenant.Options{
		Name:        "team-beta",
		Namespaces:  []string{"team-beta-dev", "team-beta-staging", "team-beta-prod"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-beta-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["project.yaml"])
}

func TestGenerateArgoCDManifests_AppGitHub(t *testing.T) {
	t.Parallel()
	opts := tenant.Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-alpha-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["app.yaml"])
}

func TestGenerateArgoCDManifests_AppGitLab(t *testing.T) {
	t.Parallel()
	opts := tenant.Options{
		Name:        "team-gamma",
		Namespaces:  []string{"team-gamma"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "gitlab",
		GitRepo:     "org/team-gamma-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["app.yaml"])
}

func TestGenerateArgoCDManifests_NoRBACConfigMap(t *testing.T) {
	t.Parallel()
	opts := tenant.Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-alpha-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	// argocd-rbac-cm.yaml should not be generated per-tenant.
	_, exists := result["argocd-rbac-cm.yaml"]
	require.False(t, exists, "argocd-rbac-cm.yaml should not be generated per-tenant")
}

func TestMergeArgoCDRBACPolicy_EmptyExisting(t *testing.T) {
	t.Parallel()
	result, err := tenant.MergeArgoCDRBACPolicy("", "team-alpha")
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_ExistingSameTenant(t *testing.T) {
	t.Parallel()
	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	result, err := tenant.MergeArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.Contains(t, result, "role:team-alpha")
	// Should not duplicate the policy lines
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_ExistingOtherTenant(t *testing.T) {
	t.Parallel()
	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	result, err := tenant.MergeArgoCDRBACPolicy(existing, "team-beta")
	require.NoError(t, err)
	require.Contains(t, result, "role:team-alpha")
	require.Contains(t, result, "role:team-beta")
	snaps.MatchSnapshot(t, result)
}

func TestRemoveArgoCDRBACPolicy_RemovesTargetTenant(t *testing.T) {
	t.Parallel()
	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team-alpha")
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_SubstringTenantNames(t *testing.T) {
	t.Parallel()
	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team, applications, *, team/*, allow
    p, role:team, projects, get, team, allow
    g, team, role:team
`
	// "team-alpha" must be added even though "team" is a substring of "team-alpha".
	result, err := tenant.MergeArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.Contains(t, result, "role:team,")
	require.Contains(t, result, "role:team-alpha,")
}

func TestRemoveArgoCDRBACPolicy_SubstringTenantNames(t *testing.T) {
	t.Parallel()
	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: |
    p, role:team, applications, *, team/*, allow
    p, role:team, projects, get, team, allow
    g, team, role:team
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	// Removing "team" should NOT remove "team-alpha" lines.
	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team,")
	require.Contains(t, result, "role:team-alpha,")
}

func TestRemoveArgoCDRBACPolicy_PreservesOtherTenant(t *testing.T) {
	t.Parallel()
	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
    p, role:team-beta, applications, *, team-beta/*, allow
    p, role:team-beta, projects, get, team-beta, allow
    g, team-beta, role:team-beta
`
	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team-alpha")
	require.Contains(t, result, "role:team-beta")
	snaps.MatchSnapshot(t, result)
}

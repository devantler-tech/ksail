package tenant

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerateArgoCDManifests_ProjectSingleNamespace(t *testing.T) {
	t.Parallel()
	opts := Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  TenantTypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-alpha-app",
	}
	result, err := GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["project.yaml"])
}

func TestGenerateArgoCDManifests_ProjectMultipleNamespaces(t *testing.T) {
	t.Parallel()
	opts := Options{
		Name:        "team-beta",
		Namespaces:  []string{"team-beta-dev", "team-beta-staging", "team-beta-prod"},
		TenantType:  TenantTypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-beta-app",
	}
	result, err := GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["project.yaml"])
}

func TestGenerateArgoCDManifests_AppGitHub(t *testing.T) {
	t.Parallel()
	opts := Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  TenantTypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-alpha-app",
	}
	result, err := GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["app.yaml"])
}

func TestGenerateArgoCDManifests_AppGitLab(t *testing.T) {
	t.Parallel()
	opts := Options{
		Name:        "team-gamma",
		Namespaces:  []string{"team-gamma"},
		TenantType:  TenantTypeArgoCD,
		GitProvider: "gitlab",
		GitRepo:     "org/team-gamma-app",
	}
	result, err := GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["app.yaml"])
}

func TestGenerateArgoCDManifests_RBACConfigMap(t *testing.T) {
	t.Parallel()
	opts := Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  TenantTypeArgoCD,
		GitProvider: "github",
		GitRepo:     "org/team-alpha-app",
	}
	result, err := GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["argocd-rbac-cm.yaml"])
}

func TestMergeArgoCDRBACPolicy_EmptyExisting(t *testing.T) {
	t.Parallel()
	result, err := MergeArgoCDRBACPolicy("", "team-alpha")
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_ExistingSameTenant(t *testing.T) {
	t.Parallel()
	existing := `apiVersion: v1
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
	result, err := MergeArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.Contains(t, result, "role:team-alpha")
	// Should not duplicate the policy lines
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_ExistingOtherTenant(t *testing.T) {
	t.Parallel()
	existing := `apiVersion: v1
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
	result, err := MergeArgoCDRBACPolicy(existing, "team-beta")
	require.NoError(t, err)
	require.Contains(t, result, "role:team-alpha")
	require.Contains(t, result, "role:team-beta")
	snaps.MatchSnapshot(t, result)
}

func TestRemoveArgoCDRBACPolicy_RemovesTargetTenant(t *testing.T) {
	t.Parallel()
	existing := `apiVersion: v1
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
	result, err := RemoveArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team-alpha")
	snaps.MatchSnapshot(t, result)
}

func TestRemoveArgoCDRBACPolicy_PreservesOtherTenant(t *testing.T) {
	t.Parallel()
	existing := `apiVersion: v1
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
	result, err := RemoveArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team-alpha")
	require.Contains(t, result, "role:team-beta")
	snaps.MatchSnapshot(t, result)
}

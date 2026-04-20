package tenant_test

import (
	"os"
	"strings"
	"testing"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestGenerateRBACManifests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts tenant.Options
	}{
		{
			name: "single namespace defaults to tenant name",
			opts: tenant.Options{
				Name:        "team-alpha",
				Namespaces:  []string{"team-alpha"},
				ClusterRole: "edit",
			},
		},
		{
			name: "multiple namespaces produce multi-doc YAML",
			opts: tenant.Options{
				Name:        "team-beta",
				Namespaces:  []string{"ns-dev", "ns-staging", "ns-prod"},
				ClusterRole: "edit",
			},
		},
		{
			name: "custom cluster role",
			opts: tenant.Options{
				Name:        "admin-tenant",
				Namespaces:  []string{"admin-tenant"},
				ClusterRole: "cluster-admin",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertRBACManifests(t, testCase.opts)
		})
	}
}

func assertRBACManifests(t *testing.T, opts tenant.Options) {
	t.Helper()

	result, err := tenant.GenerateRBACManifests(opts)
	require.NoError(t, err)

	require.Len(t, result, 3)
	require.Contains(t, result, "namespace.yaml")
	require.Contains(t, result, "serviceaccount.yaml")
	require.Contains(t, result, "rolebinding.yaml")

	// Verify YAML structure for each file
	for _, filename := range []string{
		"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml",
	} {
		content := result[filename]
		require.NotEmpty(t, content)
		require.Contains(t, content, "apiVersion:")
		require.Contains(t, content, "kind:")
		require.Contains(t, content, "metadata:")
	}

	// Verify labels on all resources
	for _, content := range result {
		require.Contains(t, content,
			"app.kubernetes.io/managed-by: ksail")
	}

	// Verify ServiceAccount name matches tenant name
	require.Contains(t, result["serviceaccount.yaml"],
		"name: "+opts.Name)

	// Verify RoleBinding references correct ClusterRole
	require.Contains(t, result["rolebinding.yaml"],
		"name: "+opts.ClusterRole)

	// Verify RoleBinding references ServiceAccount
	require.Contains(t, result["rolebinding.yaml"],
		"kind: ServiceAccount")

	// Snapshot each file
	snaps.MatchSnapshot(t, result["namespace.yaml"])
	snaps.MatchSnapshot(t, result["serviceaccount.yaml"])
	snaps.MatchSnapshot(t, result["rolebinding.yaml"])
}

func TestGenerateRBACManifestsMultiDocSeparator(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "team-multi",
		Namespaces:  []string{"ns1", "ns2"},
		ClusterRole: "edit",
	}

	result, err := tenant.GenerateRBACManifests(opts)
	require.NoError(t, err)

	// Namespaces and RoleBindings are multi-doc (one per namespace).
	for _, filename := range []string{"namespace.yaml", "rolebinding.yaml"} {
		content := result[filename]
		docs := strings.Split(content, "---\n")
		require.Len(t, docs, 2,
			"expected 2 documents in %s", filename)
	}

	// ServiceAccount is single (only in primary namespace).
	saDocs := strings.Split(result["serviceaccount.yaml"], "---\n")
	require.Len(t, saDocs, 1,
		"expected 1 document in serviceaccount.yaml")
}

package tenant

import (
	"os"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestGenerateRBACManifests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "single namespace defaults to tenant name",
			opts: Options{
				Name:        "team-alpha",
				Namespaces:  []string{"team-alpha"},
				ClusterRole: "edit",
			},
		},
		{
			name: "multiple namespaces produce multi-doc YAML",
			opts: Options{
				Name:        "team-beta",
				Namespaces:  []string{"ns-dev", "ns-staging", "ns-prod"},
				ClusterRole: "edit",
			},
		},
		{
			name: "custom cluster role",
			opts: Options{
				Name:        "admin-tenant",
				Namespaces:  []string{"admin-tenant"},
				ClusterRole: "cluster-admin",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := GenerateRBACManifests(tt.opts)
			require.NoError(t, err)

			require.Len(t, result, 3)
			require.Contains(t, result, "namespace.yaml")
			require.Contains(t, result, "serviceaccount.yaml")
			require.Contains(t, result, "rolebinding.yaml")

			// Verify YAML structure for each file
			for _, filename := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml"} {
				content := result[filename]
				require.NotEmpty(t, content)
				require.Contains(t, content, "apiVersion:")
				require.Contains(t, content, "kind:")
				require.Contains(t, content, "metadata:")
			}

			// Verify labels on all resources
			for _, content := range result {
				require.Contains(t, content, "app.kubernetes.io/managed-by: ksail")
			}

			// Verify ServiceAccount name matches tenant name
			require.Contains(t, result["serviceaccount.yaml"], "name: "+tt.opts.Name)

			// Verify RoleBinding references correct ClusterRole
			require.Contains(t, result["rolebinding.yaml"], "name: "+tt.opts.ClusterRole)

			// Verify RoleBinding references ServiceAccount
			require.Contains(t, result["rolebinding.yaml"], "kind: ServiceAccount")

			// Snapshot each file
			snaps.MatchSnapshot(t, result["namespace.yaml"])
			snaps.MatchSnapshot(t, result["serviceaccount.yaml"])
			snaps.MatchSnapshot(t, result["rolebinding.yaml"])
		})
	}
}

func TestGenerateRBACManifestsMultiDocSeparator(t *testing.T) {
	t.Parallel()

	opts := Options{
		Name:        "team-multi",
		Namespaces:  []string{"ns1", "ns2"},
		ClusterRole: "edit",
	}

	result, err := GenerateRBACManifests(opts)
	require.NoError(t, err)

	// Namespaces and RoleBindings are multi-doc (one per namespace).
	for _, filename := range []string{"namespace.yaml", "rolebinding.yaml"} {
		content := result[filename]
		docs := strings.Split(content, "---\n")
		require.Len(t, docs, 2, "expected 2 documents in %s", filename)
	}

	// ServiceAccount is single (only in primary namespace).
	saDocs := strings.Split(result["serviceaccount.yaml"], "---\n")
	require.Len(t, saDocs, 1, "expected 1 document in serviceaccount.yaml")
}

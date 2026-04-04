package tenant

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerateFluxSyncManifests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "oci source with registry and git-repo",
			opts: Options{
				Name:       "team-alpha",
				Namespaces: []string{"team-alpha-ns", "team-alpha-extras"},
				SyncSource: SyncSourceOCI,
				Registry:   "oci://ghcr.io",
				GitRepo:    "acme-org/team-alpha-manifests",
				TenantType: TenantTypeFlux,
			},
		},
		{
			name: "git source with github provider",
			opts: Options{
				Name:        "team-beta",
				Namespaces:  []string{"team-beta-ns"},
				SyncSource:  SyncSourceGit,
				GitProvider: "github",
				GitRepo:     "acme-org/team-beta-config",
				TenantType:  TenantTypeFlux,
			},
		},
		{
			name: "git source with gitlab provider",
			opts: Options{
				Name:        "team-gamma",
				Namespaces:  []string{"team-gamma-ns"},
				SyncSource:  SyncSourceGit,
				GitProvider: "gitlab",
				GitRepo:     "acme-org/team-gamma-config",
				TenantType:  TenantTypeFlux,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := GenerateFluxSyncManifests(tt.opts)
			require.NoError(t, err)
			require.Contains(t, result, "sync.yaml")
			snaps.MatchSnapshot(t, result["sync.yaml"])
		})
	}
}

func TestGenerateFluxSyncManifests_SourceKindReference(t *testing.T) {
	t.Parallel()

	t.Run("OCI source references OCIRepository in Kustomization", func(t *testing.T) {
		t.Parallel()

		result, err := GenerateFluxSyncManifests(Options{
			Name:       "oci-tenant",
			Namespaces: []string{"oci-ns"},
			SyncSource: SyncSourceOCI,
			Registry:   "oci://ghcr.io",
			GitRepo:    "org/repo",
			TenantType: TenantTypeFlux,
		})
		require.NoError(t, err)

		syncYAML := result["sync.yaml"]
		require.Contains(t, syncYAML, "kind: OCIRepository")
		require.Contains(t, syncYAML, "kind: Kustomization")

		// Verify the Kustomization sourceRef references OCIRepository
		docs := strings.Split(syncYAML, "---\n")
		require.Len(t, docs, 2)
		require.Contains(t, docs[1], "kind: OCIRepository")
	})

	t.Run("Git source references GitRepository in Kustomization", func(t *testing.T) {
		t.Parallel()

		result, err := GenerateFluxSyncManifests(Options{
			Name:        "git-tenant",
			Namespaces:  []string{"git-ns"},
			SyncSource:  SyncSourceGit,
			GitProvider: "github",
			GitRepo:     "org/repo",
			TenantType:  TenantTypeFlux,
		})
		require.NoError(t, err)

		syncYAML := result["sync.yaml"]
		require.Contains(t, syncYAML, "kind: GitRepository")
		require.Contains(t, syncYAML, "kind: Kustomization")

		docs := strings.Split(syncYAML, "---\n")
		require.Len(t, docs, 2)
		require.Contains(t, docs[1], "kind: GitRepository")
	})
}

func TestGenerateFluxSyncManifests_Labels(t *testing.T) {
	t.Parallel()

	result, err := GenerateFluxSyncManifests(Options{
		Name:       "labeled-tenant",
		Namespaces: []string{"labeled-ns"},
		SyncSource: SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		GitRepo:    "org/repo",
		TenantType: TenantTypeFlux,
	})
	require.NoError(t, err)

	syncYAML := result["sync.yaml"]
	docs := strings.Split(syncYAML, "---\n")
	require.Len(t, docs, 2)

	// Both documents must contain the managed-by label
	for _, doc := range docs {
		require.Contains(t, doc, "app.kubernetes.io/managed-by: ksail")
	}
}

func TestGenerateFluxSyncManifests_PrimaryNamespace(t *testing.T) {
	t.Parallel()

	result, err := GenerateFluxSyncManifests(Options{
		Name:       "multi-ns-tenant",
		Namespaces: []string{"primary-ns", "secondary-ns", "tertiary-ns"},
		SyncSource: SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		GitRepo:    "org/repo",
		TenantType: TenantTypeFlux,
	})
	require.NoError(t, err)

	syncYAML := result["sync.yaml"]

	// Primary namespace should appear as the namespace for all resources
	require.Contains(t, syncYAML, "namespace: primary-ns")

	// Secondary namespaces should NOT appear in sync manifests
	require.NotContains(t, syncYAML, "namespace: secondary-ns")
	require.NotContains(t, syncYAML, "namespace: tertiary-ns")
}

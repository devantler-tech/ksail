package tenant_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerateFluxSyncManifests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts tenant.Options
	}{
		{
			name: "oci source with registry and tenant-repo",
			opts: tenant.Options{
				Name:       "team-alpha",
				Namespaces: []string{"team-alpha-ns", "team-alpha-extras"},
				SyncSource: tenant.SyncSourceOCI,
				Registry:   "oci://ghcr.io",
				TenantRepo: "acme-org/team-alpha-manifests",
				TenantType: tenant.TypeFlux,
			},
		},
		{
			name: "git source with github provider",
			opts: tenant.Options{
				Name:        "team-beta",
				Namespaces:  []string{"team-beta-ns"},
				SyncSource:  tenant.SyncSourceGit,
				GitProvider: "github",
				TenantRepo:  "acme-org/team-beta-config",
				TenantType:  tenant.TypeFlux,
			},
		},
		{
			name: "git source with gitlab provider",
			opts: tenant.Options{
				Name:        "team-gamma",
				Namespaces:  []string{"team-gamma-ns"},
				SyncSource:  tenant.SyncSourceGit,
				GitProvider: "gitlab",
				TenantRepo:  "acme-org/team-gamma-config",
				TenantType:  tenant.TypeFlux,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := tenant.GenerateFluxSyncManifests(testCase.opts)
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

		result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
			Name:       "oci-tenant",
			Namespaces: []string{"oci-ns"},
			SyncSource: tenant.SyncSourceOCI,
			Registry:   "oci://ghcr.io",
			TenantRepo: "org/repo",
			TenantType: tenant.TypeFlux,
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

		result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
			Name:        "git-tenant",
			Namespaces:  []string{"git-ns"},
			SyncSource:  tenant.SyncSourceGit,
			GitProvider: "github",
			TenantRepo:  "org/repo",
			TenantType:  tenant.TypeFlux,
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

	result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
		Name:       "labeled-tenant",
		Namespaces: []string{"labeled-ns"},
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		TenantRepo: "org/repo",
		TenantType: tenant.TypeFlux,
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

	result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
		Name:       "multi-ns-tenant",
		Namespaces: []string{"primary-ns", "secondary-ns", "tertiary-ns"},
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		TenantRepo: "org/repo",
		TenantType: tenant.TypeFlux,
	})
	require.NoError(t, err)

	syncYAML := result["sync.yaml"]

	// Primary namespace should appear as the namespace for all resources
	require.Contains(t, syncYAML, "namespace: primary-ns")

	// Secondary namespaces should NOT appear in sync manifests
	require.NotContains(t, syncYAML, "namespace: secondary-ns")
	require.NotContains(t, syncYAML, "namespace: tertiary-ns")
}

func TestGenerateFluxSyncManifests_CustomSourceDirectory(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
		Name:            "custom-dir-tenant",
		Namespaces:      []string{"custom-ns"},
		SyncSource:      tenant.SyncSourceOCI,
		Registry:        "oci://ghcr.io",
		TenantRepo:      "org/repo",
		TenantType:      tenant.TypeFlux,
		SourceDirectory: "deploy",
	})
	require.NoError(t, err)

	syncYAML := result["sync.yaml"]
	require.Contains(t, syncYAML, "path: ./deploy")
	require.NotContains(t, syncYAML, "path: ./k8s")
}

func TestGenerateFluxSyncManifests_OCIPath(t *testing.T) {
	t.Parallel()

	t.Run("appends path suffix to OCI URL", func(t *testing.T) {
		t.Parallel()

		result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
			Name:       "path-tenant",
			Namespaces: []string{"path-ns"},
			SyncSource: tenant.SyncSourceOCI,
			Registry:   "oci://ghcr.io",
			TenantRepo: "org/app",
			OCIPath:    "deploy",
			TenantType: tenant.TypeFlux,
		})
		require.NoError(t, err)
		require.Contains(t, result["sync.yaml"], "url: oci://ghcr.io/org/app/deploy")
	})

	t.Run("trims leading and trailing slashes from oci-path", func(t *testing.T) {
		t.Parallel()

		result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
			Name:       "trim-tenant",
			Namespaces: []string{"trim-ns"},
			SyncSource: tenant.SyncSourceOCI,
			Registry:   "oci://ghcr.io",
			TenantRepo: "org/app",
			OCIPath:    "/manifests/",
			TenantType: tenant.TypeFlux,
		})
		require.NoError(t, err)
		require.Contains(t, result["sync.yaml"], "url: oci://ghcr.io/org/app/manifests")
	})

	t.Run("empty oci-path preserves original URL", func(t *testing.T) {
		t.Parallel()

		result, err := tenant.GenerateFluxSyncManifests(tenant.Options{
			Name:       "nopath-tenant",
			Namespaces: []string{"nopath-ns"},
			SyncSource: tenant.SyncSourceOCI,
			Registry:   "oci://ghcr.io",
			TenantRepo: "org/app",
			OCIPath:    "",
			TenantType: tenant.TypeFlux,
		})
		require.NoError(t, err)
		require.Contains(t, result["sync.yaml"], "url: oci://ghcr.io/org/app")
		require.NotContains(t, result["sync.yaml"], "url: oci://ghcr.io/org/app/")
	})
}

package tenant_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func gitVerifyTenant() tenant.Options {
	return tenant.Options{
		Name:        "team-delta",
		Namespaces:  []string{"team-delta-ns"},
		SyncSource:  tenant.SyncSourceGit,
		GitProvider: "github",
		TenantRepo:  "acme-org/team-delta-config",
		TenantType:  tenant.TypeFlux,
	}
}

// TestGenerateFluxSyncManifests_VerifiesSignedCommits snapshots the opt-in verify block
// on a Git-synced Flux tenant's GitRepository.
func TestGenerateFluxSyncManifests_VerifiesSignedCommits(t *testing.T) {
	t.Parallel()

	opts := gitVerifyTenant()
	opts.GitVerifySecretRef = "team-delta-signing-keys"
	opts.ResolveDefaults()
	require.NoError(t, opts.Validate())

	result, err := tenant.GenerateFluxSyncManifests(opts)
	require.NoError(t, err)
	require.Contains(t, result["sync.yaml"],
		"  verify:\n    mode: HEAD\n    secretRef:\n      name: team-delta-signing-keys\n")
	snaps.MatchSnapshot(t, result["sync.yaml"])
}

func TestGenerateFluxSyncManifests_UnsignedGitTenantIsUnchanged(t *testing.T) {
	t.Parallel()

	result, err := tenant.GenerateFluxSyncManifests(gitVerifyTenant())
	require.NoError(t, err)
	require.NotContains(t, result["sync.yaml"], "verify:")
}

// TestGitVerifySecretRefIsFluxGitOnly refuses commit verification wherever KSail does not
// emit a Flux GitRepository, rather than silently dropping it.
func TestGitVerifySecretRefIsFluxGitOnly(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		mutate  func(*tenant.Options)
		wantErr error
	}{
		{
			name: "flux-oci",
			mutate: func(o *tenant.Options) {
				o.SyncSource = tenant.SyncSourceOCI
				o.Registry = "oci://ghcr.io"
			},
			wantErr: tenant.ErrGitVerifyRequiresGitSync,
		},
		{
			name:    "argocd",
			mutate:  func(o *tenant.Options) { o.TenantType = tenant.TypeArgoCD },
			wantErr: tenant.ErrGitVerifyRequiresGitSync,
		},
		{
			name:    "invalid-name",
			mutate:  func(o *tenant.Options) { o.GitVerifySecretRef = "Signing_Keys" },
			wantErr: tenant.ErrInvalidGitVerifySecretRef,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := gitVerifyTenant()
			opts.GitVerifySecretRef = "signing-keys"
			testCase.mutate(&opts)
			opts.ResolveDefaults()

			require.ErrorIs(t, opts.Validate(), testCase.wantErr)
		})
	}
}

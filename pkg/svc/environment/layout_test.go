package environment_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveMultiClusterLayout_SeedsBaseAndOverlay(t *testing.T) {
	t.Parallel()

	files, err := environment.DeriveMultiClusterLayout("prod")
	require.NoError(t, err)
	require.Len(t, files, 2)

	base := files[0]
	overlay := files[1]

	assert.Equal(t, "clusters/base/kustomization.yaml", base.RelPath)
	require.NotNil(t, base.Kustomization)
	assert.Empty(t, base.Kustomization.Resources,
		"the shared base starts empty; KSail creates GitOps resources server-side")

	assert.Equal(t, "clusters/prod/kustomization.yaml", overlay.RelPath)
	require.NotNil(t, overlay.Kustomization)
	assert.Equal(t, []string{"../base"}, overlay.Kustomization.Resources,
		"the overlay references the shared base via its sibling path")
}

func TestDeriveMultiClusterLayout_OverlayPathMatchesAddEnvironmentConvention(t *testing.T) {
	t.Parallel()

	// The overlay must live at clusters/<env>/ so `project add-environment --from
	// <env>` (which clones <sourceDir>/clusters/<from>/) has an overlay to clone.
	files, err := environment.DeriveMultiClusterLayout("staging")
	require.NoError(t, err)
	require.Len(t, files, 2)

	assert.Equal(t, "clusters/staging/kustomization.yaml", files[1].RelPath)
}

func TestDeriveMultiClusterLayout_RejectsInvalidNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		envName string
		wantErr error
	}{
		{
			name:    "empty name",
			envName: "",
			wantErr: environment.ErrEmptyEnvironmentName,
		},
		{
			name:    "reserved base name collides with the shared base",
			envName: "base",
			wantErr: environment.ErrReservedEnvironmentName,
		},
		{
			name:    "path traversal token is not DNS-1123",
			envName: "..",
			wantErr: nil, // invalid-name error, asserted via require.Error below
		},
		{
			name:    "slash is not DNS-1123",
			envName: "a/b",
			wantErr: nil,
		},
		{
			name:    "uppercase is not DNS-1123",
			envName: "Prod",
			wantErr: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, err := environment.DeriveMultiClusterLayout(testCase.envName)

			require.Error(t, err)
			assert.Nil(t, files)

			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			}
		})
	}
}

// TestDeriveMultiClusterLayout_ExportedConstants pins the conventional names so a
// future scaffolder/CLI increment and the add-environment command stay aligned on
// the clusters/<env>/ + clusters/base/ layout.
func TestDeriveMultiClusterLayout_ExportedConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "clusters", environment.ClustersDir)
	assert.Equal(t, "base", environment.BaseEnvName)
}

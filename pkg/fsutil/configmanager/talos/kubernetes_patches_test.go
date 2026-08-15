package talos_test

import (
	"strings"
	"testing"

	talos "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type variantDisableCNICase struct {
	name            string
	content         string
	wantMigrated    bool
	wantContains    []string
	wantNotContains []string
}

// TestMigrateKubernetesPatchesForContract_VariantDisableCNI verifies that a
// disable-default-CNI edit is detected structurally (by cluster.network.cni.name: none),
// not only by an exact byte match, so a variant patch that carries comments or additional
// cluster.network edits is still migrated to the Talos 1.14 KubeFlannelCNIConfig delete
// document. See ksail#6167 (deferred from #5775).
func TestMigrateKubernetesPatchesForContract_VariantDisableCNI(t *testing.T) {
	t.Parallel()

	tests := []variantDisableCNICase{
		{
			name:            "exact legacy patch still migrates (regression)",
			content:         "cluster:\n  network:\n    cni:\n      name: none\n",
			wantMigrated:    true,
			wantContains:    []string{"kind: KubeFlannelCNIConfig", "$patch: delete"},
			wantNotContains: []string{"name: none"},
		},
		{
			name:            "comment-prefixed patch migrates",
			content:         "# disable the built-in CNI\ncluster:\n  network:\n    cni:\n      name: none\n",
			wantMigrated:    true,
			wantContains:    []string{"kind: KubeFlannelCNIConfig", "$patch: delete"},
			wantNotContains: []string{"name: none"},
		},
		{
			name: "patch with sibling cluster.network edit migrates and preserves the edit",
			content: "cluster:\n  network:\n    podSubnets:\n      - 10.99.0.0/16\n" +
				"    cni:\n      name: none\n",
			wantMigrated: true,
			wantContains: []string{
				"kind: KubeFlannelCNIConfig",
				"$patch: delete",
				"10.99.0.0/16",
			},
			wantNotContains: []string{"name: none"},
		},
		{
			name:            "non-none CNI is left untouched",
			content:         "cluster:\n  network:\n    cni:\n      name: flannel\n",
			wantMigrated:    false,
			wantContains:    []string{"name: flannel"},
			wantNotContains: []string{"KubeFlannelCNIConfig"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assertVariantDisableCNI(t, testCase)
		})
	}
}

func assertVariantDisableCNI(t *testing.T, testCase variantDisableCNICase) {
	t.Helper()

	patches := []talos.Patch{{Path: "cni.yaml", Content: []byte(testCase.content)}}

	migrated, err := talos.MigrateKubernetesPatchesForContract(
		patches,
		talosconfig.TalosVersion1_14,
	)
	require.NoError(t, err)
	require.Len(t, migrated, 1)

	got := string(migrated[0].Content)

	if testCase.wantMigrated {
		assert.NotEqual(t, testCase.content, got, "expected the patch to be migrated")
	} else {
		assert.Equal(t, testCase.content, got, "expected the patch to be left untouched")
	}

	for _, want := range testCase.wantContains {
		assert.Contains(t, got, want)
	}

	for _, notWant := range testCase.wantNotContains {
		assert.NotContains(t, got, notWant)
	}
}

// TestMigrateKubernetesPatchesForContract_APIServerVariantUnaffectedByCNI verifies that a
// pure API-server patch (no CNI edit) is not treated as a disable-CNI patch.
func TestMigrateKubernetesPatchesForContract_APIServerVariantUnaffectedByCNI(t *testing.T) {
	t.Parallel()

	content := "cluster:\n  apiServer:\n    extraArgs:\n      audit-log-maxage: \"30\"\n"
	patches := []talos.Patch{{Path: "api-server.yaml", Content: []byte(content)}}

	migrated, err := talos.MigrateKubernetesPatchesForContract(
		patches,
		talosconfig.TalosVersion1_14,
	)
	require.NoError(t, err)
	require.Len(t, migrated, 1)

	got := string(migrated[0].Content)
	assert.NotContains(t, got, "KubeFlannelCNIConfig")
	assert.True(t,
		strings.Contains(got, "KubeAPIServerConfig") || strings.Contains(got, "audit-log-maxage"))
}

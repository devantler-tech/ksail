package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

type nameOverrideCase struct {
	name            string
	distribution    v1alpha1.Distribution
	context         string
	fromFlag        bool
	existingCluster bool
	wantContext     string
}

// TestApplyResolvedNameOverride pins which name overrides retarget the
// connection context. Create and --name always derive it from the new name; for
// diff and update a configured metadata.name names the cluster but keeps an
// explicit context, such as a custom or OIDC context, and still derives a blank
// one. In every case the cluster name itself resolves to the override.
//
//nolint:funlen // one table keeps the whole create/flag/metadata matrix visible
func TestApplyResolvedNameOverride(t *testing.T) {
	t.Parallel()

	testCases := []nameOverrideCase{
		{
			name:         "create derives the context from a configured name",
			distribution: v1alpha1.DistributionVanilla,
			context:      "custom-context",
			wantContext:  "kind-named",
		},
		{
			name:            "update keeps an explicit context for a configured name",
			distribution:    v1alpha1.DistributionVanilla,
			context:         "custom-context",
			existingCluster: true,
			wantContext:     "custom-context",
		},
		{
			name:            "update derives a blank context from a configured name",
			distribution:    v1alpha1.DistributionVanilla,
			existingCluster: true,
			wantContext:     "kind-named",
		},
		{
			name:            "update treats a whitespace context as blank",
			distribution:    v1alpha1.DistributionVanilla,
			context:         "  ",
			existingCluster: true,
			wantContext:     "kind-named",
		},
		{
			name:            "--name retargets an explicit context on update",
			distribution:    v1alpha1.DistributionVanilla,
			context:         "custom-context",
			fromFlag:        true,
			existingCluster: true,
			wantContext:     "kind-named",
		},
		{
			name:            "Talos update keeps an explicit OIDC context",
			distribution:    v1alpha1.DistributionTalos,
			context:         "oidc@named",
			existingCluster: true,
			wantContext:     "oidc@named",
		},
		{
			name:         "Talos create derives the admin context",
			distribution: v1alpha1.DistributionTalos,
			context:      "oidc@named",
			wantContext:  "admin@named",
		},
		{
			name:            "K3s update keeps an explicit context",
			distribution:    v1alpha1.DistributionK3s,
			context:         "custom-context",
			existingCluster: true,
			wantContext:     "custom-context",
		},
		{
			// AKS has no renamed distribution config here: its cluster name is read
			// back from the context, so only the derived context keeps the name.
			name:            "AKS update keeps the context that carries its name",
			distribution:    v1alpha1.DistributionAKS,
			context:         "custom-context",
			existingCluster: true,
			wantContext:     "named",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := newNameOverrideContext(t, testCase.distribution, testCase.context)

			require.NoError(t, cluster.ExportApplyResolvedNameOverride(
				ctx, "named", testCase.fromFlag, testCase.existingCluster,
			))

			assert.Equal(t, testCase.wantContext, ctx.ClusterCfg.Spec.Cluster.Connection.Context)
			assert.Equal(t, "named", cluster.ExportResolveClusterNameFromContext(ctx))
		})
	}
}

// TestApplyResolvedNameOverride_NoNameLeavesContext proves a configuration
// without any name override keeps its context untouched for every target.
func TestApplyResolvedNameOverride_NoNameLeavesContext(t *testing.T) {
	t.Parallel()

	for _, existingCluster := range []bool{false, true} {
		ctx := newNameOverrideContext(t, v1alpha1.DistributionVanilla, "custom-context")

		require.NoError(t, cluster.ExportApplyResolvedNameOverride(ctx, "", false, existingCluster))
		assert.Equal(t, "custom-context", ctx.ClusterCfg.Spec.Cluster.Connection.Context)
		assert.Equal(t, "old", ctx.KindConfig.Name)
	}
}

func newNameOverrideContext(
	t *testing.T,
	distribution v1alpha1.Distribution,
	connectionContext string,
) *localregistry.Context {
	t.Helper()

	provider := v1alpha1.ProviderDocker
	if distribution == v1alpha1.DistributionAKS {
		provider = v1alpha1.ProviderAzure
	}

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: distribution,
					Provider:     provider,
					Connection:   v1alpha1.Connection{Context: connectionContext},
				},
			},
		},
	}

	switch distribution {
	case v1alpha1.DistributionVanilla:
		ctx.KindConfig = &kindv1alpha4.Cluster{Name: "old"}
	case v1alpha1.DistributionK3s:
		ctx.K3dConfig = &v1alpha5.SimpleConfig{}
		ctx.K3dConfig.Name = "old"
	case v1alpha1.DistributionTalos:
		talosConfig, err := talosconfigmanager.NewDefaultConfigs()
		require.NoError(t, err)

		ctx.TalosConfig = talosConfig
	case v1alpha1.DistributionVCluster, v1alpha1.DistributionKWOK, v1alpha1.DistributionEKS,
		v1alpha1.DistributionGKE, v1alpha1.DistributionAKS:
		// No renamed distribution config is needed for these cases.
	}

	return ctx
}

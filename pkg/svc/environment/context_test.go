package environment_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveContextRewrite_PerDistribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		wantOld      string
		wantNew      string
	}{
		{"Talos", v1alpha1.DistributionTalos, "admin@prod", "admin@staging"},
		{"Vanilla", v1alpha1.DistributionVanilla, "kind-prod", "kind-staging"},
		{"K3s", v1alpha1.DistributionK3s, "k3d-prod", "k3d-staging"},
		{
			"VCluster",
			v1alpha1.DistributionVCluster,
			"vcluster-docker_prod",
			"vcluster-docker_staging",
		},
		{"KWOK", v1alpha1.DistributionKWOK, "kwok-prod", "kwok-staging"},
		// EKS yields only the .eksctl.io suffix; the value-exact rewrite is still
		// well-formed and simply no-ops against a real eksctl context (see the
		// SourceContextNoOp test below).
		{"EKS", v1alpha1.DistributionEKS, "prod.eksctl.io", "staging.eksctl.io"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rewrite, ok := environment.DeriveContextRewrite(
				testCase.distribution,
				"prod",
				"staging",
			)
			require.True(t, ok)
			assert.Equal(t, environment.MetaFieldValue, rewrite.Kind)
			assert.Equal(t, "context", rewrite.Field)
			assert.Equal(t, testCase.wantOld, rewrite.Old)
			assert.Equal(t, testCase.wantNew, rewrite.New)
		})
	}
}

func TestDeriveContextRewrite_SkippedWhenContextUnresolvable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		srcName      string
		dstName      string
	}{
		{"empty source name", v1alpha1.DistributionTalos, "", "staging"},
		{"empty destination name", v1alpha1.DistributionTalos, "prod", ""},
		{"unknown distribution", v1alpha1.Distribution("Bogus"), "prod", "staging"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rewrite, ok := environment.DeriveContextRewrite(
				testCase.distribution, testCase.srcName, testCase.dstName,
			)
			require.False(t, ok)
			assert.Equal(t, environment.Rewrite{}, rewrite)
		})
	}
}

// TestDeriveContextRewrite_AppliedToConfig confirms the rewrite, composed with the
// config-rewrite set and applied through RewriteOverlayFile, repoints the
// connection context value-exactly while leaving sibling fields untouched.
func TestDeriveContextRewrite_AppliedToConfig(t *testing.T) {
	t.Parallel()

	const config = `apiVersion: cluster.ksail.io/v1alpha1
kind: Cluster
metadata:
  name: prod
spec:
  connection:
    context: admin@prod
  cluster:
    distribution: Talos
`

	rewrite, ok := environment.DeriveContextRewrite(v1alpha1.DistributionTalos, "prod", "staging")
	require.True(t, ok)

	_, newContent, err := environment.RewriteOverlayFile(
		"ksail.prod.yaml",
		config,
		[]environment.Rewrite{rewrite},
	)
	require.NoError(t, err)
	assert.Contains(t, newContent, "context: admin@staging")
	assert.NotContains(t, newContent, "admin@prod")
	// The sibling distribution scalar must be preserved.
	assert.Contains(t, newContent, "distribution: Talos")
}

// TestDeriveContextRewrite_SourceContextNoOp confirms the value-exact guarantee:
// an EKS context rewrite leaves a full eksctl context (which never equals the
// scaffold-time <name>.eksctl.io suffix) untouched.
func TestDeriveContextRewrite_SourceContextNoOp(t *testing.T) {
	t.Parallel()

	const config = `spec:
  connection:
    context: user@prod.us-east-1.eksctl.io
`

	rewrite, ok := environment.DeriveContextRewrite(v1alpha1.DistributionEKS, "prod", "staging")
	require.True(t, ok)

	_, newContent, err := environment.RewriteOverlayFile(
		"ksail.prod.yaml",
		config,
		[]environment.Rewrite{rewrite},
	)
	require.NoError(t, err)
	assert.Equal(t, config, newContent)
}

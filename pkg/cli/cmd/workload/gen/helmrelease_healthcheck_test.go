package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

// TestGenHelmReleaseWithHealthCheckExprs writes Flux's spec.healthCheckExprs. CEL
// commas stay inside one value, fields for one resource merge into one entry in
// first-seen order, and a kind-less target applies to the whole API group.
func TestGenHelmReleaseWithHealthCheckExprs(t *testing.T) {
	t.Parallel()

	output, errOutput, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"sealed-secrets",
		"--chart-ref=OCIRepository/sealed-secrets-chart",
		"--health-check-expr=bitnami.com/v1alpha1/SealedSecret=" +
			"status.conditions.filter(e, e.type == 'Synced').all(e, e.status == 'True')",
		"--health-check-expr=fluxcd.controlplane.io/v1=" +
			"status.conditions.filter(e, e.type == 'Ready').all(e, e.status == 'True')",
		"--health-check-expr=bitnami.com/v1alpha1/SealedSecret:failed=" +
			"status.conditions.filter(e, e.type == 'Synced').all(e, e.status == 'False')",
		"--health-check-expr=v1/ConfigMap:inProgress=!has(data.ready)",
		"--health-check-expr=v1/ConfigMap:current=data.ready == 'true'",
		"--export",
	})

	require.NoError(t, err)
	require.Contains(t, errOutput, "generated HelmRelease")
	snaps.MatchSnapshot(t, output)
}

func TestGenHelmReleaseWithoutHealthCheckExprsOmitsField(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewHelmReleaseCmd, []string{
		"podinfo", "--source=HelmRepository/podinfo", "--chart=podinfo", "--export",
	})

	require.NoError(t, err)
	require.NotContains(t, output, "healthCheckExprs")
}

func TestGenHelmReleaseRejectsInvalidHealthCheckExprs(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, want string
		values     []string
	}{
		{name: "missing-expression", values: []string{"v1/ConfigMap"}, want: "expected <apiVersion>"},
		{name: "empty-expression", values: []string{"v1/ConfigMap= "}, want: "expected <apiVersion>"},
		{name: "invalid-api-version", values: []string{"Bitnami/SealedSecret=true"}, want: "not an API version"},
		{name: "empty-target", values: []string{"=true"}, want: "not an API version"},
		{name: "unknown-field", values: []string{"v1/ConfigMap:ready=true"}, want: "unknown field"},
		{
			name:   "duplicate-field",
			values: []string{"v1/ConfigMap=true", "v1/ConfigMap:current=false"},
			want:   "current is set more than once",
		},
		{
			name:   "missing-current",
			values: []string{"bitnami.com/v1alpha1/SealedSecret:failed=true"},
			want:   "has no current expression",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			args := make([]string, 0, 4+len(testCase.values))

			args = append(args, "webapp", "--source=HelmRepository/charts", "--chart=webapp")
			for _, value := range testCase.values {
				args = append(args, "--health-check-expr="+value)
			}

			output, _, err := execGen(t, gen.NewHelmReleaseCmd, append(args, "--export"))

			require.Error(t, err)
			require.ErrorContains(t, err, "invalid health-check-expr")
			require.ErrorContains(t, err, testCase.want)
			require.Empty(t, output, "an invalid health check must not write a manifest")
		})
	}
}

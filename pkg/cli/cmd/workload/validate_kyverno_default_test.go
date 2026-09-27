package workload_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errKyvernoConfigLoad is a static sentinel for the config-load-failure case.
var errKyvernoConfigLoad = errors.New("read ksail.yaml")

func clusterWithPolicyEngine(engine v1alpha1.PolicyEngine) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.PolicyEngine = engine

	return cfg
}

//nolint:funlen // Table-driven test with every precedence case.
func TestResolveKyvernoPolicies(t *testing.T) {
	t.Parallel()

	kyverno := clusterWithPolicyEngine(v1alpha1.PolicyEngineKyverno)

	tests := []struct {
		name        string
		cfg         *v1alpha1.Cluster
		configFound bool
		loadErr     error
		flag        bool
		fromConfig  bool
		want        bool
	}{
		{
			name: "unset flag and Kyverno engine turns the check on",
			cfg:  kyverno, configFound: true, fromConfig: true, want: true,
		},
		{
			name: "explicit false wins over a Kyverno engine",
			cfg:  kyverno, configFound: true, flag: false, fromConfig: false, want: false,
		},
		{
			name: "explicit true wins without a Kyverno engine",
			cfg:  clusterWithPolicyEngine(v1alpha1.PolicyEngineNone), configFound: true,
			flag: true, fromConfig: false, want: true,
		},
		{
			name: "unset flag and no policy engine leaves it off",
			cfg:  clusterWithPolicyEngine(""), configFound: true, fromConfig: true, want: false,
		},
		{
			name: "unset flag and Gatekeeper engine leaves it off",
			cfg:  clusterWithPolicyEngine(v1alpha1.PolicyEngineGatekeeper), configFound: true,
			fromConfig: true, want: false,
		},
		{
			name: "unset flag and no config file leaves it off",
			cfg:  kyverno, configFound: false, fromConfig: true, want: false,
		},
		{
			name: "unset flag and a config that failed to load leaves it off",
			cfg:  nil, configFound: true, loadErr: errKyvernoConfigLoad, fromConfig: true,
			want: false,
		},
		{
			// Push-time validation passes its flags without the config derivation.
			name: "a caller that does not derive from config keeps its value",
			cfg:  kyverno, configFound: true, flag: false, fromConfig: false, want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportResolveKyvernoPolicies(
				testCase.cfg, testCase.configFound, testCase.loadErr,
				testCase.flag, testCase.fromConfig,
			)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// writePolicyEngineConfig writes a ksail.yaml declaring engine as the cluster's
// policy engine into a fresh directory and makes it the working directory, so
// validate discovers it.
func writePolicyEngineConfig(t *testing.T, engine string) {
	t.Helper()

	dir := t.TempDir()
	config := `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: test
spec:
  cluster:
    policyEngine: ` + engine + `
`

	require.NoError(t, os.WriteFile(filepath.Join(dir, "ksail.yaml"), []byte(config), 0o600))
	t.Chdir(dir)
}

//nolint:paralleltest // t.Chdir cannot be combined with t.Parallel.
func TestValidateKyvernoOnByDefaultForKyvernoEngine(t *testing.T) {
	source := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), false)

	writePolicyEngineConfig(t, "Kyverno")

	_, err := runValidate(t, source)
	require.Error(t, err, "a Kyverno cluster's own enforced policy must be checked by default")
	require.ErrorContains(t, err, `policy "require-team-label" rule "check-team" failed`)
}

//nolint:paralleltest // t.Chdir cannot be combined with t.Parallel.
func TestValidateKyvernoDefaultCanBeTurnedOff(t *testing.T) {
	source := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), false)

	writePolicyEngineConfig(t, "Kyverno")

	out, err := runValidate(t, source, "--kyverno-policies=false")
	require.NoError(t, err, "--kyverno-policies=false must turn the default check off")
	assert.NotContains(t, out, "require-team-label")
}

//nolint:paralleltest // t.Chdir cannot be combined with t.Parallel.
func TestValidateKyvernoStaysOffForOtherEngines(t *testing.T) {
	source := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), false)

	writePolicyEngineConfig(t, "None")

	out, err := runValidate(t, source)
	require.NoError(t, err, "without Kyverno as the engine the policy must not be evaluated")
	assert.NotContains(t, out, "require-team-label")
}

//nolint:paralleltest // t.Chdir cannot be combined with t.Parallel.
func TestValidateKyvernoDefaultPassesCompliantSource(t *testing.T) {
	source := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), true)

	writePolicyEngineConfig(t, "Kyverno")

	_, err := runValidate(t, source)
	require.NoError(t, err, "a document that satisfies the policy must pass the default check")
}

package workload_test

import (
	"errors"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// errRulesConfigLoad is a static sentinel for the config-load-failure case
// (err113 forbids inline errors.New in tests).
var errRulesConfigLoad = errors.New("read ksail.yaml")

func newClusterWithRules(rules string) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Workload.Validation.Rules = rules

	return cfg
}

//nolint:funlen // Table-driven test with comprehensive precedence cases.
func TestResolveCELRulesPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         *v1alpha1.Cluster
		configFound bool
		loadErr     error
		rulesFlag   string
		want        string
	}{
		{
			name:        "flag overrides config",
			cfg:         newClusterWithRules("config-rules.yaml"),
			configFound: true,
			rulesFlag:   "flag-rules.yaml",
			want:        "flag-rules.yaml",
		},
		{
			name:        "empty flag falls back to config",
			cfg:         newClusterWithRules("config-rules.yaml"),
			configFound: true,
			rulesFlag:   "",
			want:        "config-rules.yaml",
		},
		{
			name:        "empty flag and no config yields empty",
			cfg:         &v1alpha1.Cluster{},
			configFound: false,
			rulesFlag:   "",
			want:        "",
		},
		{
			name:        "empty flag and unset config field yields empty",
			cfg:         newClusterWithRules(""),
			configFound: true,
			rulesFlag:   "",
			want:        "",
		},
		{
			name:        "flag still wins when config load failed",
			cfg:         &v1alpha1.Cluster{},
			configFound: false,
			loadErr:     errRulesConfigLoad,
			rulesFlag:   "flag-rules.yaml",
			want:        "flag-rules.yaml",
		},
		{
			name:        "config load failure with empty flag yields empty (warn, not fail)",
			cfg:         &v1alpha1.Cluster{},
			configFound: false,
			loadErr:     errRulesConfigLoad,
			rulesFlag:   "",
			want:        "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			got := workload.ExportResolveCELRulesPath(
				cmd, testCase.cfg, testCase.configFound, testCase.loadErr, testCase.rulesFlag,
			)
			assert.Equal(t, testCase.want, got)
		})
	}
}

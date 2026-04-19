package setup_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/cli/setup"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallCNI_KWOKSkipsCNIInstallation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cni         v1alpha1.CNI
		wantWarning bool
	}{
		{name: "KWOK with Cilium skips CNI and warns", cni: v1alpha1.CNICilium, wantWarning: true},
		{name: "KWOK with Calico skips CNI and warns", cni: v1alpha1.CNICalico, wantWarning: true},
		{name: "KWOK with Default skips CNI silently", cni: v1alpha1.CNIDefault, wantWarning: false},
		{name: "KWOK with empty CNI skips CNI silently", cni: "", wantWarning: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionKWOK,
						CNI:          testCase.cni,
					},
				},
			}

			installed, err := setup.InstallCNI(cmd, clusterCfg, nil)
			require.NoError(t, err)
			assert.False(t, installed, "CNI should not be installed for KWOK")

			if testCase.wantWarning {
				assert.Contains(t, buf.String(), "not installed on KWOK",
					"expected a KWOK CNI skip warning in output")
			} else {
				assert.Empty(t, buf.String(), "expected no output for default/empty CNI on KWOK")
			}
		})
	}
}

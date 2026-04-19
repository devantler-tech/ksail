package setup_test

import (
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
		name string
		cni  v1alpha1.CNI
	}{
		{name: "KWOK with Cilium skips CNI", cni: v1alpha1.CNICilium},
		{name: "KWOK with Calico skips CNI", cni: v1alpha1.CNICalico},
		{name: "KWOK with Default skips CNI", cni: v1alpha1.CNIDefault},
		{name: "KWOK with empty CNI skips CNI", cni: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}

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
		})
	}
}

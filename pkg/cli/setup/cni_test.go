package setup_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/cli/setup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallCNI_KWOKSkipsCNI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cni  v1alpha1.CNI
	}{
		{name: "KWOK with Cilium skips CNI", cni: v1alpha1.CNICilium},
		{name: "KWOK with Calico skips CNI", cni: v1alpha1.CNICalico},
		{name: "KWOK with Default CNI skips CNI", cni: v1alpha1.CNIDefault},
		{name: "KWOK with empty CNI skips CNI", cni: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionKWOK,
						CNI:          testCase.cni,
					},
				},
			}

			// cmd can be nil because the KWOK guard returns before accessing it.
			installed, err := setup.InstallCNI(nil, clusterCfg, nil)

			require.NoError(t, err)
			assert.False(t, installed, "KWOK should never install CNI")
		})
	}
}

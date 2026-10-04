package clusterprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
)

// TestK3kClusterName pins the k3k cluster name for K3s on the Kubernetes
// provider. A conventional context carries the name; a custom context, which
// diff and update keep for a named cluster (ksail#7382), does not, so the
// configured metadata.name names the cluster instead.
func TestK3kClusterName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		context      string
		metadataName string
		want         string
	}{
		{name: "k3k context", context: "k3k-dev", metadataName: "named", want: "dev"},
		{name: "k3d context", context: "k3d-dev", want: "dev"},
		{
			name:         "custom context uses metadata.name",
			context:      "oidc-dev",
			metadataName: "named",
			want:         "named",
		},
		{name: "blank context uses metadata.name", metadataName: "named", want: "named"},
		{name: "unnamed custom context is used as is", context: "oidc-dev", want: "oidc-dev"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := &v1alpha1.Cluster{Spec: v1alpha1.Spec{Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{Context: testCase.context},
			}}}
			cluster.Name = testCase.metadataName

			assert.Equal(t, testCase.want, clusterprovisioner.ExportK3kClusterName(cluster))
		})
	}
}

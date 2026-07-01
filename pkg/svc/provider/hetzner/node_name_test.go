package hetzner_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		nodeType    string
		index       int
		want        string
	}{
		{
			name:        "control-plane node zero",
			clusterName: "my-cluster",
			nodeType:    hetzner.NodeTypeControlPlane,
			index:       0,
			want:        "my-cluster-controlplane-0",
		},
		{
			name:        "worker node",
			clusterName: "my-cluster",
			nodeType:    hetzner.NodeTypeWorker,
			index:       2,
			want:        "my-cluster-worker-2",
		},
		{
			name:        "name exactly at the limit is accepted",
			clusterName: strings.Repeat("a", hetzner.MaxNodeNameLength-len("-worker-0")),
			nodeType:    hetzner.NodeTypeWorker,
			index:       0,
			want: strings.Repeat("a", hetzner.MaxNodeNameLength-len("-worker-0")) +
				"-worker-0",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := hetzner.NodeName(testCase.clusterName, testCase.nodeType, testCase.index)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
			assert.LessOrEqual(t, len(got), hetzner.MaxNodeNameLength)
		})
	}
}

func TestNodeNameTooLong(t *testing.T) {
	t.Parallel()

	// A cluster name at the 63-char cap plus the "-controlplane-0" suffix exceeds
	// the DNS-1123 label limit, so the composed name is rejected.
	clusterName := strings.Repeat("a", hetzner.MaxNodeNameLength)

	name, err := hetzner.NodeName(clusterName, hetzner.NodeTypeControlPlane, 0)

	require.Error(t, err)
	require.ErrorIs(t, err, hetzner.ErrNodeNameTooLong)
	// The over-long name is still returned alongside the error for the message.
	assert.Greater(t, len(name), hetzner.MaxNodeNameLength)
}

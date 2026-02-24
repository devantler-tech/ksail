package vclusterprovisioner_test

import (
	"testing"

	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantText string
	}{
		{
			name:     "ErrNoVClusterNodes",
			err:      vclusterprovisioner.ErrNoVClusterNodes,
			wantText: "no VCluster nodes found for cluster",
		},
		{
			name:     "ErrExecFailed_reexported",
			err:      vclusterprovisioner.ErrExecFailed,
			wantText: registry.ErrExecFailed.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, tt.err, "error should not be nil")
			assert.Equal(t, tt.wantText, tt.err.Error(), "error message should match")
		})
	}
}

func TestErrors_Identity(t *testing.T) {
	t.Parallel()

	assert.ErrorIs(t, vclusterprovisioner.ErrExecFailed, registry.ErrExecFailed,
		"ErrExecFailed should be identical to registry.ErrExecFailed")
}

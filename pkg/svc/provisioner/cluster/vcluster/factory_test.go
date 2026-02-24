package vclusterprovisioner_test

import (
	"testing"

	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProvisioner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterName    string
		valuesPath     string
		disableFlannel bool
		wantErr        bool
	}{
		{
			name:           "successful_creation_with_defaults",
			clusterName:    "test-cluster",
			valuesPath:     "",
			disableFlannel: false,
			wantErr:        false,
		},
		{
			name:           "successful_creation_with_values_path",
			clusterName:    "test-cluster",
			valuesPath:     "/path/to/values.yaml",
			disableFlannel: false,
			wantErr:        false,
		},
		{
			name:           "successful_creation_with_flannel_disabled",
			clusterName:    "test-cluster",
			valuesPath:     "",
			disableFlannel: true,
			wantErr:        false,
		},
		{
			name:           "empty_name_uses_default",
			clusterName:    "",
			valuesPath:     "",
			disableFlannel: false,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provisioner, err := vclusterprovisioner.CreateProvisioner(
				tt.clusterName,
				tt.valuesPath,
				tt.disableFlannel,
			)

			if tt.wantErr {
				require.Error(t, err, "CreateProvisioner() should return error")
				assert.Nil(t, provisioner, "provisioner should be nil on error")
			} else {
				require.NoError(t, err, "CreateProvisioner() should not return error")
				assert.NotNil(t, provisioner, "provisioner should not be nil")
			}
		})
	}
}

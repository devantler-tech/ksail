package vclusterprovisioner_test

import (
	"testing"

	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProvisioner_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterName    string
		valuesPath     string
		disableFlannel bool
	}{
		{
			name:           "with_all_options",
			clusterName:    "test-cluster",
			valuesPath:     "/path/to/values.yaml",
			disableFlannel: true,
		},
		{
			name:           "with_minimal_options",
			clusterName:    "",
			valuesPath:     "",
			disableFlannel: false,
		},
		{
			name:           "with_flannel_disabled",
			clusterName:    "my-cluster",
			valuesPath:     "",
			disableFlannel: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Skip this test in CI environments where Docker is not available
			// This is a factory test that creates a real Docker client
			t.Skip("Skipping factory test - requires Docker")

			prov, err := vclusterprovisioner.CreateProvisioner(
				tt.clusterName,
				tt.valuesPath,
				tt.disableFlannel,
			)

			// In environments without Docker, this will fail
			// In local dev with Docker, this should succeed
			if err != nil {
				assert.Contains(t, err.Error(), "Docker",
					"Error should be related to Docker when Docker is unavailable")
			} else {
				require.NotNil(t, prov, "Provisioner should be created")
			}
		})
	}
}

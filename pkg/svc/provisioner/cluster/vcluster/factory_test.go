package vclusterprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/require"
)

func TestCreateProvisioner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		clusterName    string
		valuesPath     string
		disableFlannel bool
	}{
		{
			name:           "successful_creation_with_defaults",
			clusterName:    "test-cluster",
			valuesPath:     "",
			disableFlannel: false,
		},
		{
			name:           "successful_creation_with_values_path",
			clusterName:    "test-cluster",
			valuesPath:     "/path/to/values.yaml",
			disableFlannel: false,
		},
		{
			name:           "successful_creation_with_flannel_disabled",
			clusterName:    "test-cluster",
			valuesPath:     "",
			disableFlannel: true,
		},
		{
			name:           "empty_name_uses_default",
			clusterName:    "",
			valuesPath:     "",
			disableFlannel: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockProvider := provider.NewMockProvider()
			provisioner := vclusterprovisioner.NewProvisioner(
				testCase.clusterName,
				testCase.valuesPath,
				testCase.disableFlannel,
				mockProvider,
			)

			require.NotNil(t, provisioner, "provisioner should not be nil")
		})
	}
}

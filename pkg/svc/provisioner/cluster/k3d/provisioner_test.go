package k3dprovisioner_test

import (
	"context"
	"testing"

	k3dprovisioner "github.com/devantler-tech/ksail/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestK3dClusterProvisioner_List tests the List method of K3dClusterProvisioner
func TestK3dClusterProvisioner_List(t *testing.T) {
	t.Parallel()

	// Test with nil config
	t.Run("list with nil config", func(t *testing.T) {
		t.Parallel()

		provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, "")
		result, err := provisioner.List(context.Background())

		require.NoError(t, err)
		assert.NotNil(t, result)
		// In test environment without actual k3d clusters, this should be empty
		assert.Empty(t, result)
	})

	// Test with empty config path
	t.Run("list with empty config path", func(t *testing.T) {
		t.Parallel()

		provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, "")
		result, err := provisioner.List(context.Background())

		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

// TestK3dClusterProvisioner_Exists tests the Exists method
func TestK3dClusterProvisioner_Exists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		expectError bool
	}{
		{
			name:        "check existence with cluster name",
			clusterName: "test-cluster",
			expectError: false,
		},
		{
			name:        "check existence without cluster name",
			clusterName: "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, "")
			exists, err := provisioner.Exists(context.Background(), tt.clusterName)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// In test environment without actual k3d clusters, this should be false
				assert.False(t, exists)
			}
		})
	}
}

// TestK3dClusterProvisioner_NewProvisioner tests the constructor
func TestK3dClusterProvisioner_NewProvisioner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configPath string
	}{
		{
			name:       "create with config path",
			configPath: "/path/to/config",
		},
		{
			name:       "create with empty config path",
			configPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, tt.configPath)
			assert.NotNil(t, provisioner)
		})
	}
}

// TestK3dClusterProvisioner_Create tests the Create method
func TestK3dClusterProvisioner_Create(t *testing.T) {
	t.Parallel()

	// This test verifies that Create can be called without panicking
	// We don't actually create clusters in tests
	provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, "")

	// Note: This will fail because k3d binary is not available in test environment
	// But it exercises the code path
	err := provisioner.Create(context.Background(), "test-cluster")
	assert.Error(t, err) // Expected to fail without k3d
}

// TestK3dClusterProvisioner_Delete tests the Delete method
func TestK3dClusterProvisioner_Delete(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, "")

	// Note: This will fail because k3d binary is not available in test environment
	err := provisioner.Delete(context.Background(), "test-cluster")
	assert.Error(t, err) // Expected to fail without k3d
}

// TestK3dClusterProvisioner_Start tests the Start method
func TestK3dClusterProvisioner_Start(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, "")

	// Note: This will fail because k3d binary is not available in test environment
	err := provisioner.Start(context.Background(), "test-cluster")
	assert.Error(t, err) // Expected to fail without k3d
}

// TestK3dClusterProvisioner_Stop tests the Stop method
func TestK3dClusterProvisioner_Stop(t *testing.T) {
	t.Parallel()

	provisioner := k3dprovisioner.NewK3dClusterProvisioner(nil, "")

	// Note: This will fail because k3d binary is not available in test environment
	err := provisioner.Stop(context.Background(), "test-cluster")
	assert.Error(t, err) // Expected to fail without k3d
}

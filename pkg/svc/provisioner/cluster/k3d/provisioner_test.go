package k3dprovisioner_test

import (
	"context"
	"testing"

	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/assert"
)

// TestProvisioner_List tests the List method of Provisioner.
//
//nolint:paralleltest // Cannot use t.Parallel() because List() modifies global os.Stdout.
func TestProvisioner_List(t *testing.T) {
	// Test with nil config
	t.Run("list with nil config", func(t *testing.T) {
		provisioner := k3dprovisioner.NewProvisioner(nil, "")
		result, err := provisioner.List(context.Background())

		// In test environment without k3d binary, this may return an error or empty list
		// We just verify the method can be called without panicking
		if err != nil {
			// If there's an error, result may be nil which is acceptable
			t.Logf("List returned error (expected without k3d): %v", err)
		} else {
			// If no error, result should be a slice (possibly nil or empty)
			// In Go, nil slices and empty slices are both valid for "no elements"
			t.Logf("List succeeded with %d clusters", len(result))
		}
	})

	// Test with empty config path
	t.Run("list with empty config path", func(t *testing.T) {
		provisioner := k3dprovisioner.NewProvisioner(nil, "")
		result, err := provisioner.List(context.Background())

		// In test environment without k3d binary, this may return an error or empty list
		// We just verify the method can be called without panicking
		if err != nil {
			// If there's an error, result may be nil which is acceptable
			t.Logf("List returned error (expected without k3d): %v", err)
		} else {
			// If no error, result should be a slice (possibly nil or empty)
			// In Go, nil slices and empty slices are both valid for "no elements"
			t.Logf("List succeeded with %d clusters", len(result))
		}
	})
}

// TestProvisioner_Exists tests the Exists method.
//
//nolint:paralleltest // Cannot use t.Parallel() because Exists() calls List() which modifies global os.Stdout.
func TestProvisioner_Exists(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
	}{
		{
			name:        "check existence with cluster name",
			clusterName: "test-cluster",
		},
		{
			name:        "check existence without cluster name",
			clusterName: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(_ *testing.T) {
			provisioner := k3dprovisioner.NewProvisioner(nil, "")
			_, _ = provisioner.Exists(context.Background(), testCase.clusterName)

			// In test environment without actual k3d clusters, this may return an error
			// We just verify the method can be called without panicking
		})
	}
}

// TestProvisioner_NewProvisioner tests the constructor.
func TestProvisioner_NewProvisioner(t *testing.T) {
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provisioner := k3dprovisioner.NewProvisioner(nil, testCase.configPath)
			assert.NotNil(t, provisioner)
		})
	}
}

// TestProvisioner_Create tests the Create method.
func TestProvisioner_Create(t *testing.T) {
	t.Parallel()

	t.Skip("Skipping Create test - k3d binary not available in test environment")
}

// TestProvisioner_Delete tests the Delete method.
func TestProvisioner_Delete(t *testing.T) {
	t.Parallel()

	t.Skip("Skipping Delete test - k3d binary not available in test environment")
}

// TestProvisioner_Start tests the Start method.
func TestProvisioner_Start(t *testing.T) {
	t.Parallel()

	t.Skip(
		"Skipping Start test - k3d binary not available in test environment and may cause fatal errors",
	)
}

// TestProvisioner_Stop tests the Stop method.
func TestProvisioner_Stop(t *testing.T) {
	t.Parallel()

	t.Skip(
		"Skipping Stop test - k3d binary not available in test environment and may cause fatal errors",
	)
}

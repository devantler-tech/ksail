package k3d_test

import (
	"testing"

	k3dvalidator "github.com/devantler-tech/ksail/v7/pkg/fsutil/validator/k3d"
	configtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dapi "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidate_UpstreamErrors tests configs that trigger upstream K3d validation
// error paths through the full pipeline.
//
//nolint:funlen // comprehensive table-driven test
func TestValidate_UpstreamErrors(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()

	tests := []struct {
		name      string
		config    *k3dapi.SimpleConfig
		wantValid bool
	}{
		{
			name: "valid basic config",
			config: &k3dapi.SimpleConfig{
				TypeMeta: configtypes.TypeMeta{
					APIVersion: "k3d.io/v1alpha5",
					Kind:       "Simple",
				},
				ObjectMeta: configtypes.ObjectMeta{
					Name: "valid-cluster",
				},
				Servers: 1,
			},
			wantValid: true,
		},
		{
			name: "invalid api version triggers error",
			config: &k3dapi.SimpleConfig{
				TypeMeta: configtypes.TypeMeta{
					APIVersion: "invalid/v99",
					Kind:       "Simple",
				},
				ObjectMeta: configtypes.ObjectMeta{
					Name: "test",
				},
				Servers: 1,
			},
			// The upstream validation should report an error for wrong apiVersion
			wantValid: false,
		},
		{
			name: "invalid kind triggers error",
			config: &k3dapi.SimpleConfig{
				TypeMeta: configtypes.TypeMeta{
					APIVersion: "k3d.io/v1alpha5",
					Kind:       "WrongKind",
				},
				ObjectMeta: configtypes.ObjectMeta{
					Name: "test",
				},
				Servers: 1,
			},
			wantValid: false,
		},
		{
			name: "zero servers valid in K3d",
			config: &k3dapi.SimpleConfig{
				TypeMeta: configtypes.TypeMeta{
					APIVersion: "k3d.io/v1alpha5",
					Kind:       "Simple",
				},
				ObjectMeta: configtypes.ObjectMeta{
					Name: "zero-servers",
				},
				Servers: 0,
			},
			wantValid: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := validatorInstance.Validate(testCase.config)
			require.NotNil(t, result)

			if testCase.wantValid {
				assert.True(
					t,
					result.Valid,
					"expected valid result but got errors: %v",
					result.Errors,
				)
			} else {
				assert.False(t, result.Valid, "expected validation errors")
				assert.NotEmpty(t, result.Errors)
			}
		})
	}
}

// TestValidate_NilConfig tests the nil config path.
func TestValidate_NilConfig(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()
	result := validatorInstance.Validate(nil)

	require.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, result.Errors)

	found := false

	for _, err := range result.Errors {
		if err.Field == "config" && err.Message == "configuration is nil" {
			found = true
		}
	}

	assert.True(t, found, "should have nil config error")
}

// TestValidate_ConfigWithInvalidPortMapping tests configs with port mapping issues that
// may trigger upstream K3d transform or validation errors.
func TestValidate_ConfigWithInvalidPortMapping(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()

	config := &k3dapi.SimpleConfig{
		TypeMeta: configtypes.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: configtypes.ObjectMeta{
			Name: "port-test",
		},
		Servers: 1,
		Ports: []k3dapi.PortWithNodeFilters{
			{Port: "invalid-port-spec:::::"},
		},
	}

	result := validatorInstance.Validate(config)
	require.NotNil(t, result)

	assert.False(t, result.Valid, "invalid port mapping should fail validation")
	assert.NotEmpty(t, result.Errors)
}

// TestValidate_ConfigWithVolumes tests config with volume mounts that may
// exercise the transformation pipeline.
func TestValidate_ConfigWithVolumes(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()

	config := &k3dapi.SimpleConfig{
		TypeMeta: configtypes.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: configtypes.ObjectMeta{
			Name: "volume-test",
		},
		Servers: 1,
		Volumes: []k3dapi.VolumeWithNodeFilters{
			{Volume: "/nonexistent/path:/data"},
		},
	}

	result := validatorInstance.Validate(config)
	require.NotNil(t, result)
	// Non-existent volume paths are acceptable in config validation
}

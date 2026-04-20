package k3d_test

import (
	"testing"

	k3dvalidator "github.com/devantler-tech/ksail/v7/pkg/fsutil/validator/k3d"
	configtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dapi "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidate_MissingName verifies that a config with an empty name still passes
// K3d upstream validation (K3d auto-generates names when empty).
func TestValidate_MissingName(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()

	config := &k3dapi.SimpleConfig{
		TypeMeta: configtypes.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: configtypes.ObjectMeta{
			Name: "",
		},
		Servers: 1,
	}

	result := validatorInstance.Validate(config)
	require.NotNil(t, result)
	// K3d auto-generates names for empty names
	assert.True(t, result.Valid, "empty name should still be valid in K3d")
}

// TestValidate_MultipleAgents verifies a config with multiple agent nodes.
func TestValidate_MultipleAgents(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()

	config := &k3dapi.SimpleConfig{
		TypeMeta: configtypes.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: configtypes.ObjectMeta{
			Name: "agent-test",
		},
		Servers: 1,
		Agents:  3,
	}

	result := validatorInstance.Validate(config)
	require.NotNil(t, result)
	assert.True(t, result.Valid, "config with multiple agents should be valid")
}

// TestValidate_WithK3sArgs verifies a config with k3s server args.
func TestValidate_WithK3sArgs(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()

	config := &k3dapi.SimpleConfig{
		TypeMeta: configtypes.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: configtypes.ObjectMeta{
			Name: "args-test",
		},
		Servers: 1,
		Options: k3dapi.SimpleConfigOptions{
			K3sOptions: k3dapi.SimpleConfigOptionsK3s{
				ExtraArgs: []k3dapi.K3sArgWithNodeFilters{
					{Arg: "--disable=traefik", NodeFilters: []string{"server:*"}},
				},
			},
		},
	}

	result := validatorInstance.Validate(config)
	require.NotNil(t, result)
	assert.True(t, result.Valid, "config with k3s args should be valid")
	assert.Empty(t, result.Errors)
}

// TestValidate_MultipleServers verifies a config with multiple server nodes.
func TestValidate_MultipleServers(t *testing.T) {
	t.Parallel()

	validatorInstance := k3dvalidator.NewValidator()

	config := &k3dapi.SimpleConfig{
		TypeMeta: configtypes.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: configtypes.ObjectMeta{
			Name: "multi-server-test",
		},
		Servers: 3,
	}

	result := validatorInstance.Validate(config)
	require.NotNil(t, result)
	assert.True(t, result.Valid, "config with multiple servers should be valid")
}

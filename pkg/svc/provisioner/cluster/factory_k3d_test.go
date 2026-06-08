package clusterprovisioner_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// --- applyK3dNodeCounts ---
//
// Pins the CLI/cluster-level node-count override semantics: --control-planes and
// --workers override the k3d.yaml values at runtime, with the guard that when both
// are unset (<= 0) the config is left untouched.

func TestApplyK3dNodeCounts_BothUnsetLeavesConfigUntouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		controlPlanes int32
		workers       int32
	}{
		{"both zero", 0, 0},
		{"both negative", -1, -3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := &k3dv1alpha5.SimpleConfig{Servers: 5, Agents: 7}

			clusterprovisioner.ExportApplyK3dNodeCounts(config, test.controlPlanes, test.workers)

			assert.Equal(t, 5, config.Servers, "servers left untouched when both counts unset")
			assert.Equal(t, 7, config.Agents, "agents left untouched when both counts unset")
		})
	}
}

func TestApplyK3dNodeCounts_OverridesBothCounts(t *testing.T) {
	t.Parallel()

	config := &k3dv1alpha5.SimpleConfig{Servers: 0, Agents: 0}

	clusterprovisioner.ExportApplyK3dNodeCounts(config, 3, 2)

	assert.Equal(t, 3, config.Servers)
	assert.Equal(t, 2, config.Agents)
}

func TestApplyK3dNodeCounts_ControlPlanesSetWorkersZeroResetsAgents(t *testing.T) {
	t.Parallel()

	// control-planes > 0 lifts the guard, so workers == 0 is applied verbatim and
	// resets any pre-existing agent count to zero.
	config := &k3dv1alpha5.SimpleConfig{Servers: 1, Agents: 4}

	clusterprovisioner.ExportApplyK3dNodeCounts(config, 3, 0)

	assert.Equal(t, 3, config.Servers)
	assert.Equal(t, 0, config.Agents, "workers=0 with control-planes set resets agents to zero")
}

func TestApplyK3dNodeCounts_WorkersOnlyLeavesServersUntouched(t *testing.T) {
	t.Parallel()

	// control-planes <= 0 means the existing server count is preserved while workers
	// are still applied.
	config := &k3dv1alpha5.SimpleConfig{Servers: 9, Agents: 0}

	clusterprovisioner.ExportApplyK3dNodeCounts(config, 0, 4)

	assert.Equal(t, 9, config.Servers, "servers preserved when control-planes count is unset")
	assert.Equal(t, 4, config.Agents)
}

// --- writeK3dConfigToTempFile ---

func TestWriteK3dConfigToTempFile_WritesParseableConfig(t *testing.T) {
	t.Parallel()

	config := &k3dv1alpha5.SimpleConfig{Servers: 1, Agents: 2}

	path, err := clusterprovisioner.ExportWriteK3dConfigToTempFile(config)
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.Remove(path) })

	base := filepath.Base(path)
	assert.True(t, strings.HasPrefix(base, "ksail-k3d-"), "temp file uses the ksail-k3d- prefix")
	assert.True(t, strings.HasSuffix(base, ".yaml"), "temp file uses the .yaml suffix")

	data, err := os.ReadFile(path) //nolint:gosec // test path
	require.NoError(t, err)
	assert.NotEmpty(t, data, "config file should not be empty")

	var roundTripped k3dv1alpha5.SimpleConfig

	require.NoError(t, yaml.Unmarshal(data, &roundTripped), "written file is valid k3d YAML")
	assert.Equal(t, 1, roundTripped.Servers, "servers round-trip through the temp file")
	assert.Equal(t, 2, roundTripped.Agents, "agents round-trip through the temp file")
}

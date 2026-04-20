package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveClusterSpec_InvalidClusterName tests path traversal prevention.
func TestSaveClusterSpec_InvalidClusterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
	}{
		{name: "forward slash", clusterName: "../etc/passwd"},
		{name: "backslash", clusterName: "..\\etc\\passwd"},
		{name: "dot", clusterName: ".."},
		{name: "embedded dots", clusterName: "foo/../bar"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := state.SaveClusterSpec(testCase.clusterName, nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, state.ErrInvalidClusterName)
		})
	}
}

// TestLoadClusterSpec_InvalidClusterName tests path traversal prevention on load.
func TestLoadClusterSpec_InvalidClusterName(t *testing.T) {
	t.Parallel()

	_, err := state.LoadClusterSpec("../invalid")
	require.Error(t, err)
	assert.ErrorIs(t, err, state.ErrInvalidClusterName)
}

// TestDeleteClusterState_InvalidClusterName tests path traversal prevention on delete.
func TestDeleteClusterState_InvalidClusterName(t *testing.T) {
	t.Parallel()

	err := state.DeleteClusterState("../invalid")
	require.Error(t, err)
	assert.ErrorIs(t, err, state.ErrInvalidClusterName)
}

// TestLoadClusterSpec_InvalidJSON tests unmarshaling invalid JSON data.
func TestLoadClusterSpec_InvalidJSON(t *testing.T) {
	t.Parallel()

	clusterName := "test-invalid-json-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	// First, create the directory structure manually and write invalid JSON.
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	stateDir := filepath.Join(home, ".ksail", "clusters", clusterName)
	err = os.MkdirAll(stateDir, 0o700)
	require.NoError(t, err)

	specPath := filepath.Join(stateDir, "spec.json")
	err = os.WriteFile(specPath, []byte("not valid json{{{"), 0o600)
	require.NoError(t, err)

	_, err = state.LoadClusterSpec(clusterName)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal cluster spec")
}

// TestLoadClusterSpec_FileIsDirectory tests reading state when the file path is a directory.
func TestLoadClusterSpec_FileIsDirectory(t *testing.T) {
	t.Parallel()

	clusterName := "test-dir-as-file-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	// Create the directory structure with spec.json as a directory instead of a file.
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	specDir := filepath.Join(home, ".ksail", "clusters", clusterName, "spec.json")
	err = os.MkdirAll(specDir, 0o700)
	require.NoError(t, err)

	_, err = state.LoadClusterSpec(clusterName)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read cluster state")
}

// TestLoadClusterTTL_FileIsDirectory tests reading TTL when the file path is a directory.
func TestLoadClusterTTL_FileIsDirectory(t *testing.T) {
	t.Parallel()

	clusterName := "test-ttl-dir-as-file-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	// Create the directory structure with ttl.json as a directory instead of a file.
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	ttlDir := filepath.Join(home, ".ksail", "clusters", clusterName, "ttl.json")
	err = os.MkdirAll(ttlDir, 0o700)
	require.NoError(t, err)

	_, err = state.LoadClusterTTL(clusterName)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read ttl state")
}

// TestLoadClusterTTL_InvalidJSON tests unmarshaling invalid JSON TTL data.
func TestLoadClusterTTL_InvalidJSON(t *testing.T) {
	t.Parallel()

	clusterName := "test-invalid-ttl-json-" + t.Name()

	t.Cleanup(func() {
		_ = state.DeleteClusterState(clusterName)
	})

	// Create directory and write invalid JSON to ttl file.
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	stateDir := filepath.Join(home, ".ksail", "clusters", clusterName)
	err = os.MkdirAll(stateDir, 0o700)
	require.NoError(t, err)

	ttlPath := filepath.Join(stateDir, "ttl.json")
	err = os.WriteFile(ttlPath, []byte("invalid json!!!"), 0o600)
	require.NoError(t, err)

	_, err = state.LoadClusterTTL(clusterName)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal ttl info")
}

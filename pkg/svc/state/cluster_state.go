package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

const (
	// stateDir is the directory under the user's home where cluster state is stored.
	stateDir = ".ksail"
	// clustersSubDir holds per-cluster state directories.
	clustersSubDir = "clusters"
	// specFileName is the file containing the serialized ClusterSpec.
	specFileName = "spec.json"
	// dirPermissions is the permission mode for state directories.
	dirPermissions = 0o700
	// filePermissions is the permission mode for state files.
	filePermissions = 0o600
)

// ErrStateNotFound is returned when no saved state exists for a cluster.
var ErrStateNotFound = errors.New("cluster state not found")

// ErrInvalidClusterName is returned when a cluster name contains path traversal characters.
var ErrInvalidClusterName = errors.New(
	"invalid cluster name: must not contain path separators or '..'",
)

// clusterStatePath returns the path to the state file for a given cluster name.
func clusterStatePath(clusterName string) (string, error) {
	if strings.Contains(clusterName, "/") ||
		strings.Contains(clusterName, "\\") ||
		strings.Contains(clusterName, "..") {
		return "", ErrInvalidClusterName
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(home, stateDir, clustersSubDir, clusterName, specFileName), nil
}

// SaveClusterSpec persists the ClusterSpec used during cluster creation.
// This allows the update command to compare against the actual creation-time
// configuration instead of static defaults.
func SaveClusterSpec(clusterName string, spec *v1alpha1.ClusterSpec) error {
	statePath, err := clusterStatePath(clusterName)
	if err != nil {
		return err
	}

	dir := filepath.Dir(statePath)

	err = os.MkdirAll(dir, dirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create state directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cluster spec: %w", err)
	}

	err = os.WriteFile(statePath, data, filePermissions)
	if err != nil {
		return fmt.Errorf("failed to write cluster state: %w", err)
	}

	return nil
}

// LoadClusterSpec loads a previously saved ClusterSpec for a cluster.
// Returns ErrStateNotFound if no state exists for this cluster name.
func LoadClusterSpec(clusterName string) (*v1alpha1.ClusterSpec, error) {
	statePath, err := clusterStatePath(clusterName)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // path is constructed from user home + constant subpath, not user input
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrStateNotFound, clusterName)
		}

		return nil, fmt.Errorf("failed to read cluster state: %w", err)
	}

	var spec v1alpha1.ClusterSpec

	err = json.Unmarshal(data, &spec)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal cluster spec: %w", err)
	}

	return &spec, nil
}

// DeleteClusterState removes the saved state for a cluster.
// This should be called during cluster deletion to clean up.
// Returns nil if the state does not exist (idempotent).
func DeleteClusterState(clusterName string) error {
	statePath, err := clusterStatePath(clusterName)
	if err != nil {
		return err
	}

	dir := filepath.Dir(statePath)

	err = os.RemoveAll(dir)
	if err != nil {
		return fmt.Errorf("failed to remove cluster state directory: %w", err)
	}

	return nil
}

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

const (
	// EKSNodegroupStateVersion is the on-disk schema version for EKS capacity snapshots.
	EKSNodegroupStateVersion = 1
	// eksNodegroupStateFileNameFormat is kept separate from spec and TTL state so a successful start
	// can clear only the transient capacity snapshot. The region is part of the file name because a
	// cluster name is unique only within a region.
	eksNodegroupStateFileNameFormat = "eks-nodegroups-%s.json"
)

var (
	// ErrEKSNodegroupStateNotFound reports that no stop-time EKS capacity snapshot exists.
	ErrEKSNodegroupStateNotFound = errors.New("EKS nodegroup state not found")
	// ErrInvalidRegion reports a missing region or one containing path separators or '..'.
	ErrInvalidRegion = errors.New(
		"invalid AWS region: must be non-empty and must not contain path separators or '..'",
	)
	errEKSNodegroupStateNil = errors.New("EKS nodegroup state is nil")
)

// EKSNodegroupCapacity records the exact scaling values a stopped managed nodegroup must regain.
type EKSNodegroupCapacity struct {
	Name            string `json:"name"`
	DesiredCapacity int    `json:"desiredCapacity"`
	MinSize         int    `json:"minSize"`
	MaxSize         int    `json:"maxSize"`
}

// EKSNodegroupState binds a stop-time capacity snapshot to one cluster name and AWS region.
type EKSNodegroupState struct {
	Version     int                    `json:"version"`
	ClusterName string                 `json:"clusterName"`
	Region      string                 `json:"region"`
	Nodegroups  []EKSNodegroupCapacity `json:"nodegroups"`
}

// SaveEKSNodegroupState atomically persists the capacity snapshot before stop mutates AWS.
func SaveEKSNodegroupState(clusterName, region string, snapshot *EKSNodegroupState) error {
	if snapshot == nil {
		return errEKSNodegroupStateNil
	}

	statePath, err := eksNodegroupStatePath(clusterName, region)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(statePath), dirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create EKS nodegroup state directory: %w", err)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal EKS nodegroup state: %w", err)
	}

	err = fsutil.AtomicWriteFile(statePath, data, filePermissions)
	if err != nil {
		return fmt.Errorf("failed to write EKS nodegroup state: %w", err)
	}

	return nil
}

// LoadEKSNodegroupState reads the stop-time EKS capacity snapshot.
func LoadEKSNodegroupState(clusterName, region string) (*EKSNodegroupState, error) {
	statePath, err := eksNodegroupStatePath(clusterName, region)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // clusterStateDir validates the user-controlled path component.
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrEKSNodegroupStateNotFound, clusterName)
		}

		return nil, fmt.Errorf("failed to read EKS nodegroup state: %w", err)
	}

	var snapshot EKSNodegroupState

	err = json.Unmarshal(data, &snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal EKS nodegroup state: %w", err)
	}

	return &snapshot, nil
}

// DeleteEKSNodegroupState removes only the transient EKS capacity snapshot.
func DeleteEKSNodegroupState(clusterName, region string) error {
	statePath, err := eksNodegroupStatePath(clusterName, region)
	if err != nil {
		return err
	}

	err = os.Remove(statePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove EKS nodegroup state: %w", err)
	}

	return nil
}

// eksNodegroupStatePath scopes the snapshot to one cluster *and* one region. Cluster names are only
// unique within a region, so keying the file by name alone lets same-named clusters in two regions
// share — and clobber — a single snapshot: a stop in one region would be restored from the other's
// capacities, or rejected outright by the snapshot's own region identity check.
func eksNodegroupStatePath(clusterName, region string) (string, error) {
	return eksRegionScopedStatePath(clusterName, region, eksNodegroupStateFileNameFormat)
}

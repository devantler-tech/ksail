package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

const (
	// EKSComponentStateVersion is the on-disk schema version for declarative EKS component state.
	EKSComponentStateVersion = 1
	// eksComponentStateFileNameFormat is region-scoped because EKS cluster names
	// are unique only within an AWS region.
	eksComponentStateFileNameFormat = "eks-components-%s.json"
)

var (
	// ErrEKSComponentStateNotFound reports that no EKS component baseline exists.
	ErrEKSComponentStateNotFound = errors.New("EKS component state not found")
	// ErrInvalidEKSComponentState reports malformed or mismatched component state.
	ErrInvalidEKSComponentState = errors.New("invalid EKS component state")
)

// EKSComponentState preserves declarative choices that cannot be inferred
// unambiguously from the live EKS cluster.
type EKSComponentState struct {
	Version                               int                   `json:"version"`
	ClusterName                           string                `json:"clusterName"`
	Region                                string                `json:"region"`
	LoadBalancer                          v1alpha1.LoadBalancer `json:"loadBalancer"`
	ExperimentalAWSLoadBalancerController bool                  `json:"experimentalAWSLoadBalancerController"` //nolint:lll,tagliatelle // matches public EKS option key
}

// SaveEKSComponentState atomically persists an exact-region component baseline.
func SaveEKSComponentState(clusterName, region string, snapshot *EKSComponentState) error {
	statePath, err := eksComponentStatePath(clusterName, region)
	if err != nil {
		return err
	}

	err = validateEKSComponentState(clusterName, region, snapshot)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(statePath), dirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create EKS component state directory: %w", err)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal EKS component state: %w", err)
	}

	err = fsutil.AtomicWriteFile(statePath, data, filePermissions)
	if err != nil {
		return fmt.Errorf("failed to write EKS component state: %w", err)
	}

	return nil
}

// LoadEKSComponentState reads and validates an exact-region component baseline.
func LoadEKSComponentState(clusterName, region string) (*EKSComponentState, error) {
	statePath, err := eksComponentStatePath(clusterName, region)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // the cluster name and region path components are validated.
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"%w: %s in %s",
				ErrEKSComponentStateNotFound,
				clusterName,
				region,
			)
		}

		return nil, fmt.Errorf("failed to read EKS component state: %w", err)
	}

	var snapshot EKSComponentState

	err = json.Unmarshal(data, &snapshot)
	if err != nil {
		return nil, fmt.Errorf(
			"unmarshal EKS component state: %w: %w",
			err,
			ErrInvalidEKSComponentState,
		)
	}

	err = validateEKSComponentState(clusterName, region, &snapshot)
	if err != nil {
		return nil, err
	}

	return &snapshot, nil
}

func validateEKSComponentState(
	clusterName, region string,
	snapshot *EKSComponentState,
) error {
	if snapshot == nil {
		return fmt.Errorf("%w: state is nil", ErrInvalidEKSComponentState)
	}

	clusterName = strings.TrimSpace(clusterName)
	region = strings.TrimSpace(region)

	if snapshot.Version != EKSComponentStateVersion ||
		strings.TrimSpace(snapshot.ClusterName) != clusterName ||
		strings.TrimSpace(snapshot.Region) != region ||
		!slices.Contains(v1alpha1.ValidLoadBalancers(), snapshot.LoadBalancer) {
		return fmt.Errorf(
			"%w: identity, version, or load-balancer value does not match",
			ErrInvalidEKSComponentState,
		)
	}

	return nil
}

func eksComponentStatePath(clusterName, region string) (string, error) {
	return eksRegionScopedStatePath(clusterName, region, eksComponentStateFileNameFormat)
}

func eksRegionScopedStatePath(clusterName, region, fileNameFormat string) (string, error) {
	dir, err := clusterStateDir(clusterName)
	if err != nil {
		return "", err
	}

	region = strings.TrimSpace(region)
	if region == "" || strings.Contains(region, "/") || strings.Contains(region, "\\") ||
		strings.Contains(region, "..") {
		return "", ErrInvalidRegion
	}

	return filepath.Join(dir, fmt.Sprintf(fileNameFormat, region)), nil
}

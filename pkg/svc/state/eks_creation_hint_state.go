package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

const (
	// EKSCreationHintVersion is the on-disk schema version for failed-create hints.
	EKSCreationHintVersion = 1
	// eksCreationHintFileNameFormat keeps the hint region-scoped like the ownership record. Its name
	// deliberately does not match the ownership record pattern, so a hint is never listed or loaded
	// as ownership.
	eksCreationHintFileNameFormat = "eks-creation-hint-%s.json"
)

var (
	// ErrEKSCreationHintNotFound reports that no failed-create hint exists for the target.
	ErrEKSCreationHintNotFound = errors.New("EKS creation hint not found")
	// ErrInvalidEKSCreationHint reports a malformed or incomplete failed-create hint.
	ErrInvalidEKSCreationHint = errors.New("invalid EKS creation hint")
)

// EKSCreationHint keeps the creation identifier of a failed `ksail cluster create` on EKS whose
// cluster AWS did not report yet, so the cluster can still be claimed if it appears after KSail
// exited (ksail#8324). It is local and untrusted: it never authorizes a lifecycle operation on its
// own. A caller may only use it to look for a live cluster in the same account and region that
// carries exactly this identifier as its creation tag.
type EKSCreationHint struct {
	Version     int    `json:"version"`
	ClusterName string `json:"clusterName"`
	Region      string `json:"region"`
	AccountID   string `json:"accountId"`
	CreationID  string `json:"creationId"`
}

// SaveEKSCreationHint atomically persists one validated failed-create hint.
func SaveEKSCreationHint(clusterName, region string, hint *EKSCreationHint) error {
	statePath, err := eksCreationHintPath(clusterName, region)
	if err != nil {
		return err
	}

	err = validateEKSCreationHint(clusterName, region, hint)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(statePath), dirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create EKS creation hint directory: %w", err)
	}

	data, err := json.MarshalIndent(hint, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal EKS creation hint: %w", err)
	}

	err = fsutil.AtomicWriteFile(statePath, data, filePermissions)
	if err != nil {
		return fmt.Errorf("failed to write EKS creation hint: %w", err)
	}

	return nil
}

// LoadEKSCreationHint reads and validates the failed-create hint for one cluster name and region.
func LoadEKSCreationHint(clusterName, region string) (*EKSCreationHint, error) {
	statePath, err := eksCreationHintPath(clusterName, region)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // clusterStateDir and eksRegionScopedStatePath constrain both path components.
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"%w: %s in %s",
				ErrEKSCreationHintNotFound,
				clusterName,
				region,
			)
		}

		return nil, fmt.Errorf("failed to read EKS creation hint: %w", err)
	}

	var hint EKSCreationHint

	err = json.Unmarshal(data, &hint)
	if err != nil {
		return nil, fmt.Errorf(
			"unmarshal EKS creation hint: %w: %w",
			err,
			ErrInvalidEKSCreationHint,
		)
	}

	err = validateEKSCreationHint(clusterName, region, &hint)
	if err != nil {
		return nil, err
	}

	return &hint, nil
}

// DeleteEKSCreationHint removes the failed-create hint for one cluster name and region. A missing
// hint is not an error.
func DeleteEKSCreationHint(clusterName, region string) error {
	statePath, err := eksCreationHintPath(clusterName, region)
	if err != nil {
		return err
	}

	err = os.Remove(statePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove EKS creation hint: %w", err)
	}

	return nil
}

func validateEKSCreationHint(clusterName, region string, hint *EKSCreationHint) error {
	if hint == nil {
		return fmt.Errorf("%w: hint is nil", ErrInvalidEKSCreationHint)
	}

	if hint.Version != EKSCreationHintVersion {
		return fmt.Errorf(
			"%w: unsupported version %d (want %d)",
			ErrInvalidEKSCreationHint,
			hint.Version,
			EKSCreationHintVersion,
		)
	}

	if strings.TrimSpace(hint.ClusterName) != strings.TrimSpace(clusterName) ||
		strings.TrimSpace(hint.Region) != strings.TrimSpace(region) ||
		!awsAccountIDPattern.MatchString(strings.TrimSpace(hint.AccountID)) ||
		strings.TrimSpace(hint.CreationID) == "" {
		return fmt.Errorf(
			"%w: required fields are missing or do not match the state key",
			ErrInvalidEKSCreationHint,
		)
	}

	return nil
}

func eksCreationHintPath(clusterName, region string) (string, error) {
	return eksRegionScopedStatePath(clusterName, region, eksCreationHintFileNameFormat)
}

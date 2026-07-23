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
	// EKSComponentStateVersion is the on-disk schema version for declarative EKS component state.
	EKSComponentStateVersion = 3
	// eksComponentStateFileNameFormat is account-and-region-scoped because EKS cluster names are
	// unique only within one AWS account and region.
	eksComponentStateFileNameFormat = "eks-components-%s-%s.json"
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
	Version                                  int    `json:"version"`
	ClusterName                              string `json:"clusterName"`
	Region                                   string `json:"region"`
	AccountID                                string `json:"accountId"`
	AWSLoadBalancerControllerManaged         bool   `json:"awsLoadBalancerControllerManaged,omitzero"`          //nolint:lll // exact component ownership marker
	AWSLoadBalancerControllerReleaseIdentity string `json:"awsLoadBalancerControllerReleaseIdentity,omitempty"` //nolint:lll // Helm storage object UID
	AWSLoadBalancerControllerServiceAccount  string `json:"awsLoadBalancerControllerServiceAccount,omitempty"`  //nolint:lll // matches public EKS option key
}

// SaveEKSComponentState atomically persists an exact-account-and-region component baseline.
func SaveEKSComponentState(clusterName, region string, snapshot *EKSComponentState) error {
	accountID := ""
	if snapshot != nil {
		accountID = snapshot.AccountID
	}

	err := validateEKSComponentState(clusterName, region, accountID, snapshot)
	if err != nil {
		return err
	}

	statePath, err := eksComponentStatePath(clusterName, region, accountID)
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

// LoadEKSComponentState reads and validates an exact-account-and-region component baseline. Normal
// callers omit accountID so the immutable ownership record supplies it; tests and migrations may
// provide one explicit account ID without creating a live ownership binding.
func LoadEKSComponentState(
	clusterName, region string,
	accountIDs ...string,
) (*EKSComponentState, error) {
	accountID, err := resolveEKSComponentAccountID(clusterName, region, accountIDs)
	if err != nil {
		return nil, err
	}

	statePath, err := eksComponentStatePath(clusterName, region, accountID)
	if err != nil {
		return nil, err
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(statePath), statePath)
	if err != nil {
		missingState := errors.Is(err, os.ErrNotExist)
		if errors.Is(err, fsutil.ErrPathOutsideBase) {
			_, statErr := os.Stat(statePath)
			missingState = errors.Is(statErr, os.ErrNotExist)
		}

		if missingState {
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

	err = validateEKSComponentState(clusterName, region, accountID, &snapshot)
	if err != nil {
		return nil, err
	}

	return &snapshot, nil
}

func validateEKSComponentState(
	clusterName, region, accountID string,
	snapshot *EKSComponentState,
) error {
	if snapshot == nil {
		return fmt.Errorf("%w: state is nil", ErrInvalidEKSComponentState)
	}

	clusterName = strings.TrimSpace(clusterName)
	region = strings.TrimSpace(region)
	accountID = strings.TrimSpace(accountID)

	if snapshot.Version != EKSComponentStateVersion ||
		strings.TrimSpace(snapshot.ClusterName) != clusterName ||
		strings.TrimSpace(snapshot.Region) != region ||
		strings.TrimSpace(snapshot.AccountID) != accountID ||
		!awsAccountIDPattern.MatchString(accountID) {
		return fmt.Errorf(
			"%w: identity or version does not match",
			ErrInvalidEKSComponentState,
		)
	}

	hasReleaseIdentity := strings.TrimSpace(
		snapshot.AWSLoadBalancerControllerReleaseIdentity,
	) != ""
	if snapshot.AWSLoadBalancerControllerManaged != hasReleaseIdentity {
		return fmt.Errorf(
			"%w: controller ownership and release identity do not match",
			ErrInvalidEKSComponentState,
		)
	}

	return nil
}

func eksComponentStatePath(clusterName, region, accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if !awsAccountIDPattern.MatchString(accountID) {
		return "", fmt.Errorf("%w: invalid AWS account ID", ErrInvalidEKSComponentState)
	}

	return eksRegionScopedStatePath(
		clusterName,
		region,
		fmt.Sprintf(eksComponentStateFileNameFormat, accountID, "%s"),
	)
}

func resolveEKSComponentAccountID(
	clusterName, region string,
	accountIDs []string,
) (string, error) {
	if len(accountIDs) > 1 {
		return "", fmt.Errorf("%w: multiple AWS account IDs", ErrInvalidEKSComponentState)
	}

	if len(accountIDs) == 1 {
		accountID := strings.TrimSpace(accountIDs[0])
		if !awsAccountIDPattern.MatchString(accountID) {
			return "", fmt.Errorf("%w: invalid AWS account ID", ErrInvalidEKSComponentState)
		}

		return accountID, nil
	}

	ownership, err := LoadEKSOwnershipState(clusterName, region)
	if err != nil {
		if errors.Is(err, ErrEKSOwnershipStateNotFound) {
			return "", fmt.Errorf(
				"%w: %s in %s has no account binding",
				ErrEKSComponentStateNotFound,
				clusterName,
				region,
			)
		}

		return "", fmt.Errorf("resolve EKS component account binding: %w", err)
	}

	return ownership.AccountID, nil
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

// DeleteEKSRegionState removes state scoped to one exact EKS target, retaining
// same-named clusters in other AWS regions. The legacy TTL and ClusterSpec files
// are name-scoped, so they are also removed: retaining either can auto-delete or
// misconfigure a later same-named cluster, while neither can safely identify
// another region.
func DeleteEKSRegionState(clusterName, region string, accountIDs ...string) error {
	accountID, err := resolveEKSComponentAccountID(clusterName, region, accountIDs)
	if err != nil {
		return err
	}

	componentPath, err := eksComponentStatePath(clusterName, region, accountID)
	if err != nil {
		return err
	}

	nodegroupPath, err := eksNodegroupStatePath(clusterName, region)
	if err != nil {
		return err
	}

	ownershipPath, err := eksOwnershipStatePath(clusterName, region)
	if err != nil {
		return err
	}

	ttlPath, err := clusterTTLPath(clusterName)
	if err != nil {
		return err
	}

	specPath, err := clusterStatePath(clusterName)
	if err != nil {
		return err
	}

	var cleanupErrs []error

	for _, statePath := range []string{
		componentPath,
		nodegroupPath,
		ownershipPath,
		ttlPath,
		specPath,
	} {
		removeErr := os.Remove(statePath)
		if removeErr != nil && !os.IsNotExist(removeErr) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf(
				"remove EKS region state %q: %w",
				statePath,
				removeErr,
			))
		}
	}

	return errors.Join(cleanupErrs...)
}

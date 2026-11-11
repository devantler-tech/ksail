package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

// EKSCreationHint keeps the creation identifiers of failed `ksail cluster create` attempts on EKS whose
// cluster AWS did not report yet, so the cluster can still be claimed if it appears after KSail exited
// (ksail#8324). Attempts are grouped by the AWS account they ran in, and an attempt is kept until a
// lifecycle command resolves it for that account: a retry, or an attempt in another account, never
// hides an earlier attempt whose cluster may still appear. It is local and untrusted: it never
// authorizes a lifecycle operation on its own. A caller may only use it to look for a live cluster in
// its own account and this region that carries exactly one of that account's identifiers as its
// creation tag.
type EKSCreationHint struct {
	Version     int                   `json:"version"`
	ClusterName string                `json:"clusterName"`
	Region      string                `json:"region"`
	Accounts    []EKSCreationAttempts `json:"accounts"`
}

// EKSCreationAttempts lists the unresolved create attempts made in one AWS account, oldest first.
type EKSCreationAttempts struct {
	AccountID   string   `json:"accountId"`
	CreationIDs []string `json:"creationIds"`
}

// CreationIDsFor returns the unresolved attempt identifiers kept for accountID, oldest first.
func (h *EKSCreationHint) CreationIDsFor(accountID string) []string {
	accountID = strings.TrimSpace(accountID)

	for _, attempts := range h.Accounts {
		if attempts.AccountID == accountID {
			return slices.Clone(attempts.CreationIDs)
		}
	}

	return nil
}

// RecordEKSCreationAttempt adds one unresolved create attempt made in accountID to the target's hint.
// Every attempt kept earlier, for this or any other account, is retained. A hint that cannot be read
// is replaced, since nothing in it can be trusted to match.
func RecordEKSCreationAttempt(clusterName, region, accountID, creationID string) error {
	accountID = strings.TrimSpace(accountID)

	hint, err := LoadEKSCreationHint(clusterName, region)
	if err != nil {
		hint = &EKSCreationHint{
			Version:     EKSCreationHintVersion,
			ClusterName: strings.TrimSpace(clusterName),
			Region:      strings.TrimSpace(region),
		}
	}

	index := slices.IndexFunc(hint.Accounts, func(attempts EKSCreationAttempts) bool {
		return attempts.AccountID == accountID
	})
	if index < 0 {
		hint.Accounts = append(hint.Accounts, EKSCreationAttempts{AccountID: accountID})
		index = len(hint.Accounts) - 1
	}

	kept := &hint.Accounts[index]
	kept.CreationIDs = slices.DeleteFunc(kept.CreationIDs, func(id string) bool {
		return id == creationID
	})
	kept.CreationIDs = append(kept.CreationIDs, creationID)

	return SaveEKSCreationHint(clusterName, region, hint)
}

// ForgetEKSCreationAttempts removes the attempts kept for accountID once a lifecycle command resolved
// them in that account: their cluster was recorded, or AWS confirmed it does not exist. Attempts kept
// for other accounts stay. The hint file is removed when no attempts remain. A missing hint is not an
// error.
func ForgetEKSCreationAttempts(clusterName, region, accountID string) error {
	hint, err := LoadEKSCreationHint(clusterName, region)
	if errors.Is(err, ErrEKSCreationHintNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	accountID = strings.TrimSpace(accountID)
	hint.Accounts = slices.DeleteFunc(hint.Accounts, func(attempts EKSCreationAttempts) bool {
		return attempts.AccountID == accountID
	})

	if len(hint.Accounts) == 0 {
		return DeleteEKSCreationHint(clusterName, region)
	}

	return SaveEKSCreationHint(clusterName, region, hint)
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

// LoadEKSCreationHint reads and validates the failed-create hint for one cluster name and region. The
// read is confined to the hint's own directory, so a hint file that resolves elsewhere is refused.
func LoadEKSCreationHint(clusterName, region string) (*EKSCreationHint, error) {
	statePath, err := eksCreationHintPath(clusterName, region)
	if err != nil {
		return nil, err
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(statePath), statePath)
	if err != nil {
		if isMissingStateFile(statePath, err) {
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

// HasEKSCreationHint reports whether a failed-create hint file exists for the cluster in any region.
// It says nothing about whether the hint is valid or matches anything; callers use it only as local
// evidence that a KSail create was attempted for this name.
func HasEKSCreationHint(clusterName string) bool {
	dir, err := clusterStateDir(clusterName)
	if err != nil {
		return false
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	pattern := fmt.Sprintf(eksCreationHintFileNameFormat, "*")

	return slices.ContainsFunc(entries, func(entry fs.DirEntry) bool {
		matched, matchErr := filepath.Match(pattern, entry.Name())

		return matchErr == nil && matched && entry.Type().IsRegular()
	})
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

// isMissingStateFile reports whether a confined read failed because the file does not exist. The
// confined reader reports a missing file as outside its base, so that case is checked directly.
func isMissingStateFile(statePath string, readErr error) bool {
	if errors.Is(readErr, os.ErrNotExist) {
		return true
	}

	if !errors.Is(readErr, fsutil.ErrPathOutsideBase) {
		return false
	}

	_, statErr := os.Lstat(statePath)

	return errors.Is(statErr, os.ErrNotExist)
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
		len(hint.Accounts) == 0 {
		return fmt.Errorf(
			"%w: required fields are missing or do not match the state key",
			ErrInvalidEKSCreationHint,
		)
	}

	seen := make(map[string]bool, len(hint.Accounts))

	for _, attempts := range hint.Accounts {
		if !validEKSCreationAttempts(attempts) || seen[attempts.AccountID] {
			return fmt.Errorf(
				"%w: each account must be a distinct AWS account with at least one attempt",
				ErrInvalidEKSCreationHint,
			)
		}

		seen[attempts.AccountID] = true
	}

	return nil
}

func validEKSCreationAttempts(attempts EKSCreationAttempts) bool {
	return awsAccountIDPattern.MatchString(attempts.AccountID) && len(attempts.CreationIDs) > 0 &&
		!slices.ContainsFunc(attempts.CreationIDs, func(id string) bool {
			return strings.TrimSpace(id) == ""
		})
}

func eksCreationHintPath(clusterName, region string) (string, error) {
	return eksRegionScopedStatePath(clusterName, region, eksCreationHintFileNameFormat)
}

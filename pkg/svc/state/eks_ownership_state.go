package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
)

const (
	// EKSOwnershipStateVersion is the on-disk schema version for immutable EKS ownership records.
	EKSOwnershipStateVersion = 1
	// eksOwnershipStateFileNameFormat keeps ownership records scoped to the AWS region. The wider
	// per-cluster state directory remains name-keyed until its dedicated migration (ksail#6224).
	eksOwnershipStateFileNameFormat = "eks-ownership-%s.json"
)

var (
	// ErrEKSOwnershipStateNotFound reports a legacy or missing immutable EKS ownership record.
	ErrEKSOwnershipStateNotFound = errors.New("EKS ownership state not found")
	// ErrInvalidEKSOwnershipState reports malformed, incomplete, or internally inconsistent state.
	ErrInvalidEKSOwnershipState = errors.New("invalid EKS ownership state")
	awsAccountIDPattern         = regexp.MustCompile(`^[0-9]{12}$`)
)

// EKSOwnershipState binds a KSail-managed EKS target to the AWS account and exact EKS incarnation
// observed after creation. EKS ARNs are name-derived and repeat after replacement, so CreatedAt is
// part of the identity as the AWS-assigned, immutable incarnation fingerprint. No credentials are
// persisted.
type EKSOwnershipState struct {
	Version     int       `json:"version"`
	ClusterName string    `json:"clusterName"`
	Region      string    `json:"region"`
	AccountID   string    `json:"accountId"`
	ClusterARN  string    `json:"clusterArn"`
	CreatedAt   time.Time `json:"createdAt"`
	// AWSOptions stores the complete environment-variable-name mapping used to resolve AWS
	// credentials. Credential values are never persisted. Records without this mapping predate the
	// schema extension and require explicit rebind before state-only lifecycle commands may use them.
	AWSOptions v1alpha1.OptionsAWS `json:"awsOptions,omitzero"`
}

// SaveEKSOwnershipState atomically persists one validated immutable ownership record.
func SaveEKSOwnershipState(clusterName, region string, ownership *EKSOwnershipState) error {
	statePath, err := eksOwnershipStatePath(clusterName, region)
	if err != nil {
		return err
	}

	err = validateEKSOwnershipState(clusterName, region, ownership)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(statePath), dirPermissions)
	if err != nil {
		return fmt.Errorf("failed to create EKS ownership state directory: %w", err)
	}

	data, err := json.MarshalIndent(ownership, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal EKS ownership state: %w", err)
	}

	err = fsutil.AtomicWriteFile(statePath, data, filePermissions)
	if err != nil {
		return fmt.Errorf("failed to write EKS ownership state: %w", err)
	}

	return nil
}

// LoadEKSOwnershipState reads and strictly validates one immutable ownership record.
func LoadEKSOwnershipState(clusterName, region string) (*EKSOwnershipState, error) {
	statePath, err := eksOwnershipStatePath(clusterName, region)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // clusterStateDir and validateEKSRegion constrain both path components.
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"%w: %s in %s",
				ErrEKSOwnershipStateNotFound,
				clusterName,
				region,
			)
		}

		return nil, fmt.Errorf("failed to read EKS ownership state: %w", err)
	}

	var ownership EKSOwnershipState

	err = json.Unmarshal(data, &ownership)
	if err != nil {
		return nil, fmt.Errorf(
			"unmarshal EKS ownership state: %w: %w",
			err,
			ErrInvalidEKSOwnershipState,
		)
	}

	err = validateEKSOwnershipState(clusterName, region, &ownership)
	if err != nil {
		return nil, err
	}

	return &ownership, nil
}

// ListEKSOwnershipStates loads every region-scoped immutable ownership record for a cluster.
// Callers use this only when no region was resolved from config or kubeconfig; multiple records
// remain ambiguous until an explicitly configured region environment variable selects one.
func ListEKSOwnershipStates(clusterName string) ([]*EKSOwnershipState, error) {
	dir, err := clusterStateDir(clusterName)
	if err != nil {
		return nil, err
	}

	paths, err := filepath.Glob(filepath.Join(dir, "eks-ownership-*.json"))
	if err != nil {
		return nil, fmt.Errorf("list EKS ownership state: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrEKSOwnershipStateNotFound, clusterName)
	}

	ownerships := make([]*EKSOwnershipState, 0, len(paths))
	for _, path := range paths {
		//nolint:gosec // glob is rooted under the validated per-cluster state directory.
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read EKS ownership state: %w", readErr)
		}

		var ownership EKSOwnershipState
		if unmarshalErr := json.Unmarshal(data, &ownership); unmarshalErr != nil {
			return nil, fmt.Errorf(
				"unmarshal EKS ownership state: %w: %w",
				unmarshalErr,
				ErrInvalidEKSOwnershipState,
			)
		}

		region := strings.TrimSpace(ownership.Region)
		if validateErr := validateEKSOwnershipState(clusterName, region, &ownership); validateErr != nil {
			return nil, validateErr
		}

		expectedPath, pathErr := eksOwnershipStatePath(clusterName, region)
		if pathErr != nil || filepath.Clean(expectedPath) != filepath.Clean(path) {
			return nil, fmt.Errorf(
				"%w: ownership filename does not match region %q",
				ErrInvalidEKSOwnershipState,
				region,
			)
		}

		ownerships = append(ownerships, &ownership)
	}

	sort.Slice(ownerships, func(i, j int) bool {
		return ownerships[i].Region < ownerships[j].Region
	})

	return ownerships, nil
}

func validateEKSOwnershipState(clusterName, region string, ownership *EKSOwnershipState) error {
	if ownership == nil {
		return fmt.Errorf("%w: state is nil", ErrInvalidEKSOwnershipState)
	}

	clusterName = strings.TrimSpace(clusterName)
	region = strings.TrimSpace(region)

	if ownership.Version != EKSOwnershipStateVersion {
		return fmt.Errorf(
			"%w: unsupported version %d (want %d)",
			ErrInvalidEKSOwnershipState,
			ownership.Version,
			EKSOwnershipStateVersion,
		)
	}

	err := validateEKSOwnershipFields(clusterName, region, ownership)
	if err != nil {
		return err
	}

	parsedARN, err := awsarn.Parse(strings.TrimSpace(ownership.ClusterARN))
	if err != nil {
		return fmt.Errorf("%w: parse cluster ARN: %w", ErrInvalidEKSOwnershipState, err)
	}

	return validateEKSOwnershipARN(clusterName, region, ownership.AccountID, parsedARN)
}

func validateEKSOwnershipFields(
	clusterName, region string,
	ownership *EKSOwnershipState,
) error {
	if strings.TrimSpace(ownership.ClusterName) != clusterName ||
		strings.TrimSpace(ownership.Region) != region ||
		!awsAccountIDPattern.MatchString(strings.TrimSpace(ownership.AccountID)) ||
		strings.TrimSpace(ownership.ClusterARN) == "" || ownership.CreatedAt.IsZero() ||
		!hasCompleteAWSOptions(ownership.AWSOptions) {
		return fmt.Errorf(
			"%w: required identity fields are missing or do not match the state key",
			ErrInvalidEKSOwnershipState,
		)
	}

	return nil
}

func hasCompleteAWSOptions(options v1alpha1.OptionsAWS) bool {
	return strings.TrimSpace(options.ProfileEnvVar) != "" &&
		strings.TrimSpace(options.RegionEnvVar) != "" &&
		strings.TrimSpace(options.AccessKeyIDEnvVar) != "" &&
		strings.TrimSpace(options.SecretAccessKeyEnvVar) != "" &&
		strings.TrimSpace(options.SessionTokenEnvVar) != ""
}

func validateEKSOwnershipARN(
	clusterName, region, accountID string,
	parsedARN awsarn.ARN,
) error {
	if parsedARN.Service != "eks" || parsedARN.Region != region ||
		parsedARN.AccountID != strings.TrimSpace(accountID) ||
		parsedARN.Resource != "cluster/"+clusterName {
		return fmt.Errorf(
			"%w: cluster ARN does not match name, region, and account",
			ErrInvalidEKSOwnershipState,
		)
	}

	return nil
}

func eksOwnershipStatePath(clusterName, region string) (string, error) {
	dir, err := clusterStateDir(clusterName)
	if err != nil {
		return "", err
	}

	region = strings.TrimSpace(region)
	if region == "" || strings.Contains(region, "/") || strings.Contains(region, "\\") ||
		strings.Contains(region, "..") {
		return "", ErrInvalidRegion
	}

	return filepath.Join(dir, fmt.Sprintf(eksOwnershipStateFileNameFormat, region)), nil
}

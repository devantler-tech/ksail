package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	// legacyAWSOptionPlaceholder stands in for the awsOptions a pre-awsOptions record lacks, so the
	// rest of that record can still be validated.
	legacyAWSOptionPlaceholder = "LEGACY_RECORD"
)

var (
	// ErrEKSOwnershipStateNotFound reports a legacy or missing immutable EKS ownership record.
	ErrEKSOwnershipStateNotFound = errors.New("EKS ownership state not found")
	// ErrInvalidEKSOwnershipState reports malformed, incomplete, or internally inconsistent state.
	ErrInvalidEKSOwnershipState = errors.New("invalid EKS ownership state")
	// ErrEKSOwnershipStateUnreadable reports ownership records that exist but could not be read,
	// parsed, or validated, with no usable record beside them. It is deliberately distinct from
	// ErrEKSOwnershipStateNotFound: corruption must refuse, never fall back to the absence path.
	ErrEKSOwnershipStateUnreadable = errors.New("EKS ownership state present but unreadable")
	awsAccountIDPattern            = regexp.MustCompile(`^[0-9]{12}$`)
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
//
// Individually unusable records — unreadable, malformed, failing validation, or predating the
// awsOptions schema — are skipped rather than failing the whole listing, so one stale record in an
// unrelated region cannot strand a cluster whose target region is recorded correctly. When nothing
// usable survives, the result is absence — unless a record was present but could not be read,
// parsed, or validated as anything other than a legacy record. That returns
// ErrEKSOwnershipStateUnreadable, naming the files, because a record that exists but cannot be
// read is evidence that something is wrong, not evidence that none was
// written: treating it as absent would let a stale rendered config bind a cluster unopposed.
// Selecting a region from the survivors never weakens the ownership check:
// eksidentity.NewVerifier still loads and strictly validates the selected region's record.
func ListEKSOwnershipStates(clusterName string) ([]*EKSOwnershipState, error) {
	dir, err := clusterStateDir(clusterName)
	if err != nil {
		return nil, err
	}

	paths, err := eksOwnershipRecordPaths(clusterName, dir)
	if err != nil {
		return nil, err
	}

	ownerships := make([]*EKSOwnershipState, 0, len(paths))
	unreadable := []string{}

	for _, path := range paths {
		ownership, readable := loadUsableEKSOwnershipRecord(clusterName, path)
		if !readable {
			unreadable = append(unreadable, path)
		}

		if ownership != nil {
			ownerships = append(ownerships, ownership)
		}
	}

	if len(ownerships) == 0 {
		if len(unreadable) > 0 {
			return nil, fmt.Errorf(
				"%w: %s: %s",
				ErrEKSOwnershipStateUnreadable,
				clusterName,
				strings.Join(unreadable, ", "),
			)
		}

		return nil, fmt.Errorf("%w: %s", ErrEKSOwnershipStateNotFound, clusterName)
	}

	sort.Slice(ownerships, func(i, j int) bool {
		return ownerships[i].Region < ownerships[j].Region
	})

	return ownerships, nil
}

// eksOwnershipRecordPaths lists the ownership record files in a cluster's state directory.
//
// filepath.Glob is not used because it discards directory read errors, so a state directory the
// process cannot read would list as empty and report absence. Only a directory that does not exist
// means no records; any other read failure is ErrEKSOwnershipStateUnreadable.
func eksOwnershipRecordPaths(clusterName, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf(
			"%w: %s: read state directory %s: %w",
			ErrEKSOwnershipStateUnreadable,
			clusterName,
			dir,
			err,
		)
	}

	pattern := fmt.Sprintf(eksOwnershipStateFileNameFormat, "*")
	paths := make([]string, 0, len(entries))

	for _, entry := range entries {
		matched, matchErr := filepath.Match(pattern, entry.Name())
		if matchErr != nil {
			return nil, fmt.Errorf("list EKS ownership state: %w", matchErr)
		}

		if matched {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}

	return paths, nil
}

// loadUsableEKSOwnershipRecord returns the record at path, or nil when it cannot be trusted to
// contribute a credential mapping. The filename must match the region it claims, so a record cannot
// be read under another region's key. readable is false when the file could not be read, is not
// valid JSON, or parses but is not a valid ownership record. The one exception is a record in the
// schema that predated awsOptions: it is readable and simply unusable, so it keeps the absence path.
func loadUsableEKSOwnershipRecord(clusterName, path string) (*EKSOwnershipState, bool) {
	//nolint:gosec // glob is rooted under the validated per-cluster state directory.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var ownership EKSOwnershipState

	err = json.Unmarshal(data, &ownership)
	if err != nil {
		return nil, false
	}

	region := strings.TrimSpace(ownership.Region)

	if !isEKSOwnershipRecordAtPath(clusterName, region, path) {
		return nil, false
	}

	err = validateEKSOwnershipState(clusterName, region, &ownership)
	if err != nil {
		return nil, isLegacyEKSOwnershipRecord(clusterName, region, &ownership)
	}

	return &ownership, true
}

// isEKSOwnershipRecordAtPath reports whether path is the file the record's own region keys to.
func isEKSOwnershipRecordAtPath(clusterName, region, path string) bool {
	expectedPath, err := eksOwnershipStatePath(clusterName, region)

	return err == nil && filepath.Clean(expectedPath) == filepath.Clean(path)
}

// isLegacyEKSOwnershipRecord reports whether a record that failed validation is otherwise complete
// and fails only because it predates the awsOptions schema.
func isLegacyEKSOwnershipRecord(clusterName, region string, record *EKSOwnershipState) bool {
	if hasCompleteAWSOptions(record.AWSOptions) {
		return false
	}

	ownership := *record
	ownership.AWSOptions = v1alpha1.OptionsAWS{
		ProfileEnvVar:         legacyAWSOptionPlaceholder,
		RegionEnvVar:          legacyAWSOptionPlaceholder,
		AccessKeyIDEnvVar:     legacyAWSOptionPlaceholder,
		SecretAccessKeyEnvVar: legacyAWSOptionPlaceholder,
		SessionTokenEnvVar:    legacyAWSOptionPlaceholder,
	}

	return validateEKSOwnershipState(clusterName, region, &ownership) == nil
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
	return eksRegionScopedStatePath(clusterName, region, eksOwnershipStateFileNameFormat)
}

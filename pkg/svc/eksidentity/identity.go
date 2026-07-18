// Package eksidentity captures and verifies the immutable AWS identity of KSail-managed EKS
// clusters. It deliberately contains no lifecycle mutation: callers use it as a fail-closed
// precondition before delete or nodegroup scaling.
package eksidentity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsarn "github.com/aws/aws-sdk-go-v2/aws/arn"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

var (
	// ErrIdentityMismatch reports that the current AWS identity or live cluster does not match the
	// immutable ownership record captured by KSail.
	ErrIdentityMismatch = errors.New("EKS ownership identity mismatch")
	// ErrInvalidLiveIdentity reports an incomplete or internally inconsistent AWS response.
	ErrInvalidLiveIdentity = errors.New("invalid live EKS identity")
)

// Client is the read-only AWS surface needed to bind and verify EKS ownership. *pkg/client/eks.Client
// satisfies it, while tests can provide credential-free fakes.
type Client interface {
	CallerAccountID(ctx context.Context) (string, error)
	DescribeCluster(ctx context.Context, name string) (*ekstypes.Cluster, error)
}

// Verifier rechecks that the current AWS account and exact live EKS incarnation still match the
// immutable ownership record captured before a lifecycle operation began.
type Verifier func(context.Context) error

// VerifyBeforeMutation invokes verifier at the narrowest mutation boundary. A nil verifier keeps
// non-EKS and create-only paths unchanged.
func VerifyBeforeMutation(ctx context.Context, verifier Verifier) error {
	if verifier == nil {
		return nil
	}

	err := verifier(ctx)
	if err != nil {
		return fmt.Errorf("reverify immutable EKS ownership before mutation: %w", err)
	}

	return nil
}

// Capture observes the current caller and live cluster, validates their relationship, and atomically
// stores the immutable identity. It performs no AWS mutation and is suitable after create or during
// an explicit legacy-state rebind.
func Capture(
	ctx context.Context,
	client Client,
	clusterName, region string,
) (*state.EKSOwnershipState, error) {
	ownership, err := Observe(ctx, client, clusterName, region)
	if err != nil {
		return nil, err
	}

	err = Persist(clusterName, ownership.Region, ownership)
	if err != nil {
		return nil, err
	}

	return ownership, nil
}

// Persist replaces the immutable ownership identity only after removing any transient nodegroup
// capacity snapshot keyed to the same name and region. Capacity state predates immutable ownership
// and cannot safely be carried across a create or explicit rebind to another EKS incarnation.
func Persist(clusterName, region string, ownership *state.EKSOwnershipState) error {
	err := state.DeleteEKSNodegroupState(clusterName, region)
	if err != nil {
		return fmt.Errorf(
			"clear stale EKS nodegroup capacity state before ownership capture: %w",
			err,
		)
	}

	err = state.SaveEKSOwnershipState(clusterName, region, ownership)
	if err != nil {
		return fmt.Errorf("persist immutable EKS ownership identity: %w", err)
	}

	return nil
}

// NewVerifier loads the persisted ownership identity once and returns a verifier closed over that
// immutable snapshot. Reusing the returned verifier across prompts and read phases prevents a
// concurrent create or rebind from silently retargeting an in-flight mutation to a replacement.
func NewVerifier(client Client, clusterName, region string) (Verifier, error) {
	expected, err := state.LoadEKSOwnershipState(clusterName, region)
	if err != nil {
		return nil, migrationRequiredError(clusterName, err)
	}

	expectedSnapshot := *expected

	return func(ctx context.Context) error {
		return verifyExpected(ctx, client, clusterName, region, &expectedSnapshot)
	}, nil
}

// Verify fails closed unless the current AWS account and exact live EKS incarnation match the
// currently persisted ownership identity. Callers that span prompts or other read phases should use
// NewVerifier and reuse its immutable snapshot for every later mutation boundary.
func Verify(ctx context.Context, client Client, clusterName, region string) error {
	verifier, err := NewVerifier(client, clusterName, region)
	if err != nil {
		return err
	}

	return verifier(ctx)
}

func verifyExpected(
	ctx context.Context,
	client Client,
	clusterName, region string,
	expected *state.EKSOwnershipState,
) error {
	accountID, err := client.CallerAccountID(ctx)
	if err != nil {
		return fmt.Errorf("resolve current AWS account for EKS ownership verification: %w", err)
	}

	accountID = strings.TrimSpace(accountID)
	if accountID != expected.AccountID {
		return fmt.Errorf(
			"%w: current AWS account %q does not match persisted account %q",
			ErrIdentityMismatch,
			accountID,
			expected.AccountID,
		)
	}

	cluster, err := client.DescribeCluster(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("describe EKS cluster for ownership verification: %w", err)
	}

	live, err := identityFromCluster(clusterName, region, accountID, cluster)
	if err != nil {
		return err
	}

	if live.ClusterARN != expected.ClusterARN {
		return fmt.Errorf(
			"%w: live cluster ARN %q does not match persisted ARN %q",
			ErrIdentityMismatch,
			live.ClusterARN,
			expected.ClusterARN,
		)
	}

	if !live.CreatedAt.Equal(expected.CreatedAt) {
		return fmt.Errorf(
			"%w: live cluster creation time %s does not match persisted creation time %s",
			ErrIdentityMismatch,
			live.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
			expected.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		)
	}

	return nil
}

// Observe returns the validated live identity without persisting or mutating anything. It is used by
// the explicit rebind flow so users can review the target before confirming the local state write.
func Observe(
	ctx context.Context,
	client Client,
	clusterName, region string,
) (*state.EKSOwnershipState, error) {
	accountID, err := client.CallerAccountID(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve current AWS account for EKS ownership capture: %w", err)
	}

	cluster, err := client.DescribeCluster(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("describe EKS cluster for ownership capture: %w", err)
	}

	return identityFromCluster(clusterName, region, strings.TrimSpace(accountID), cluster)
}

func identityFromCluster( //nolint:cyclop // one validation boundary intentionally checks every AWS identity field.
	clusterName, region, accountID string,
	cluster *ekstypes.Cluster,
) (*state.EKSOwnershipState, error) {
	clusterName = strings.TrimSpace(clusterName)
	region = strings.TrimSpace(region)
	accountID = strings.TrimSpace(accountID)

	if cluster == nil || strings.TrimSpace(aws.ToString(cluster.Name)) != clusterName ||
		strings.TrimSpace(aws.ToString(cluster.Arn)) == "" || cluster.CreatedAt == nil ||
		cluster.CreatedAt.IsZero() {
		return nil, fmt.Errorf(
			"%w: cluster name, ARN, or creation time is missing or does not match",
			ErrInvalidLiveIdentity,
		)
	}

	clusterARN := strings.TrimSpace(aws.ToString(cluster.Arn))

	parsedARN, err := awsarn.Parse(clusterARN)
	if err != nil {
		return nil, fmt.Errorf("%w: parse cluster ARN: %w", ErrInvalidLiveIdentity, err)
	}

	if region == "" {
		region = parsedARN.Region
	}

	if parsedARN.Service != "eks" || parsedARN.Region != region ||
		parsedARN.Resource != "cluster/"+clusterName {
		return nil, fmt.Errorf(
			"%w: cluster ARN does not match the resolved name and region",
			ErrInvalidLiveIdentity,
		)
	}

	if parsedARN.AccountID != accountID {
		return nil, fmt.Errorf(
			"%w: caller account %q does not own live cluster account %q",
			ErrIdentityMismatch,
			accountID,
			parsedARN.AccountID,
		)
	}

	return &state.EKSOwnershipState{
		Version:     state.EKSOwnershipStateVersion,
		ClusterName: clusterName,
		Region:      region,
		AccountID:   accountID,
		ClusterARN:  clusterARN,
		CreatedAt:   cluster.CreatedAt.UTC(),
	}, nil
}

func migrationRequiredError(clusterName string, cause error) error {
	return fmt.Errorf(
		"immutable EKS ownership identity is missing or invalid for %q: %w; "+
			"after confirming the current AWS credentials select the intended cluster, "+
			"run `ksail cluster eks-bind --name %s --provider AWS --experimental`",
		clusterName,
		cause,
		clusterName,
	)
}

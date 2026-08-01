package eksidentity_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errIdentityQuery = errors.New("identity query failed")

// captureAWSOptions is the canonical default mapping used by Capture call sites that do not
// exercise custom credential variables.
func captureAWSOptions() v1alpha1.OptionsAWS {
	return credentials.AWSOptionsWithDefaults(v1alpha1.OptionsAWS{})
}

type fakeClient struct {
	accountID     string
	accountErr    error
	cluster       *ekstypes.Cluster
	describeErr   error
	describeCalls int
}

func (f *fakeClient) CallerAccountID(_ context.Context) (string, error) {
	return f.accountID, f.accountErr
}

func (f *fakeClient) DescribeCluster(_ context.Context, _ string) (*ekstypes.Cluster, error) {
	f.describeCalls++

	return f.cluster, f.describeErr
}

func TestCapturePersistsImmutableEKSIdentity(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{
		accountID: "123456789012",
		cluster: eksCluster(
			"capture-identity",
			"arn:aws:eks:eu-north-1:123456789012:cluster/capture-identity",
			createdAt,
		),
	}

	got, err := eksidentity.Capture(
		t.Context(), client, "capture-identity", "eu-north-1", captureAWSOptions(),
	)
	require.NoError(t, err)
	assert.Equal(t, "123456789012", got.AccountID)
	assert.Equal(t, createdAt, got.CreatedAt)

	persisted, err := state.LoadEKSOwnershipState("capture-identity", "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, got, persisted)
}

func TestCaptureClearsStaleNodegroupCapacityState(t *testing.T) {
	t.Parallel()

	const clusterName = "capture-clears-stale-nodegroups"

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	require.NoError(t, state.SaveEKSNodegroupState(
		clusterName,
		"eu-north-1",
		&state.EKSNodegroupState{
			Version:     state.EKSNodegroupStateVersion,
			ClusterName: clusterName,
			Region:      "eu-north-1",
			Nodegroups: []state.EKSNodegroupCapacity{{
				Name:            "workers",
				DesiredCapacity: 3,
				MinSize:         1,
				MaxSize:         5,
			}},
		},
	))

	client := &fakeClient{
		accountID: "123456789012",
		cluster: eksCluster(
			clusterName,
			"arn:aws:eks:eu-north-1:123456789012:cluster/"+clusterName,
			identityTime(),
		),
	}

	_, err := eksidentity.Capture(
		t.Context(), client, clusterName, "eu-north-1", captureAWSOptions(),
	)
	require.NoError(t, err)

	_, err = state.LoadEKSNodegroupState(clusterName, "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)
}

func TestCaptureFailsClosedWhenNodegroupCapacityStateCannotBeCleared(t *testing.T) {
	t.Parallel()

	const clusterName = "capture-nodegroup-clear-failure"

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	blockedStatePath := filepath.Join(
		home,
		".ksail",
		"clusters",
		clusterName,
		"eks-nodegroups-eu-north-1.json",
	)
	require.NoError(t, os.MkdirAll(filepath.Join(blockedStatePath, "blocker"), 0o700))

	client := &fakeClient{
		accountID: "123456789012",
		cluster: eksCluster(
			clusterName,
			"arn:aws:eks:eu-north-1:123456789012:cluster/"+clusterName,
			identityTime(),
		),
	}

	_, err = eksidentity.Capture(
		t.Context(), client, clusterName, "eu-north-1", captureAWSOptions(),
	)
	require.ErrorContains(t, err, "clear stale EKS nodegroup capacity state")

	_, loadErr := state.LoadEKSOwnershipState(clusterName, "eu-north-1")
	require.ErrorIs(t, loadErr, state.ErrEKSOwnershipStateNotFound)
}

func TestVerifyRejectsDifferentAccountBeforeDescribe(t *testing.T) {
	t.Parallel()

	persistOwnership(t, "account-mismatch", identityTime())

	client := &fakeClient{accountID: "210987654321"}

	err := eksidentity.Verify(t.Context(), client, "account-mismatch", "eu-north-1")
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	require.ErrorContains(t, err, "AWS account")
	assert.Zero(t, client.describeCalls)
}

func TestVerifyRejectsSameARNReplacement(t *testing.T) {
	t.Parallel()

	clusterName := "same-arn-replacement"
	arn := "arn:aws:eks:eu-north-1:123456789012:cluster/" + clusterName
	persistOwnership(t, clusterName, identityTime())
	client := &fakeClient{
		accountID: "123456789012",
		cluster: eksCluster(
			clusterName,
			arn,
			identityTime().Add(time.Minute),
		),
	}

	err := eksidentity.Verify(t.Context(), client, clusterName, "eu-north-1")
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	require.ErrorContains(t, err, "creation time")
}

func TestVerifyRejectsDifferentClusterARN(t *testing.T) {
	t.Parallel()

	clusterName := "arn-mismatch"
	persistOwnership(t, clusterName, identityTime())
	client := &fakeClient{
		accountID: "123456789012",
		cluster: eksCluster(
			clusterName,
			"arn:aws-us-gov:eks:eu-north-1:123456789012:cluster/"+clusterName,
			identityTime(),
		),
	}

	err := eksidentity.Verify(t.Context(), client, clusterName, "eu-north-1")
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	require.ErrorContains(t, err, "cluster ARN")
}

func TestCapturedVerifierRejectsReplacementAfterOwnershipStateChanges(t *testing.T) {
	t.Parallel()

	clusterName := "captured-verifier-state-replacement"

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	createdAt := identityTime()
	persistOwnership(t, clusterName, createdAt)

	client := &fakeClient{
		accountID: "123456789012",
		cluster: eksCluster(
			clusterName,
			"arn:aws:eks:eu-north-1:123456789012:cluster/"+clusterName,
			createdAt,
		),
	}

	verifier, err := eksidentity.NewVerifier(client, clusterName, "eu-north-1")
	require.NoError(t, err)
	require.NoError(t, verifier(t.Context()))

	replacementTime := createdAt.Add(time.Minute)
	persistOwnership(t, clusterName, replacementTime)
	client.cluster = eksCluster(
		clusterName,
		"arn:aws:eks:eu-north-1:123456789012:cluster/"+clusterName,
		replacementTime,
	)

	err = verifier(t.Context())
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	require.ErrorContains(t, err, "creation time")
}

func TestVerifyLegacyStateFailsClosedWithRebindCommand(t *testing.T) {
	t.Parallel()

	client := &fakeClient{accountID: "123456789012"}

	err := eksidentity.Verify(t.Context(), client, "legacy-state", "eu-north-1")
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
	require.ErrorContains(
		t,
		err,
		"ksail cluster eks-bind --name legacy-state --provider AWS --experimental",
	)
	assert.Zero(t, client.describeCalls)
}

func TestVerifyPropagatesIdentityQueriesWithoutMutation(t *testing.T) {
	t.Parallel()

	clusterName := "query-failure"
	persistOwnership(t, clusterName, identityTime())

	t.Run("caller identity", func(t *testing.T) {
		t.Parallel()

		client := &fakeClient{accountErr: errIdentityQuery}
		err := eksidentity.Verify(t.Context(), client, clusterName, "eu-north-1")
		require.ErrorIs(t, err, errIdentityQuery)
		assert.Zero(t, client.describeCalls)
	})

	t.Run("describe cluster", func(t *testing.T) {
		t.Parallel()

		client := &fakeClient{accountID: "123456789012", describeErr: errIdentityQuery}
		err := eksidentity.Verify(t.Context(), client, clusterName, "eu-north-1")
		require.ErrorIs(t, err, errIdentityQuery)
		assert.Equal(t, 1, client.describeCalls)
	})
}

func persistOwnership(t *testing.T, clusterName string, createdAt time.Time) {
	t.Helper()

	const accountID = "123456789012"

	require.NoError(t, state.SaveEKSOwnershipState(
		clusterName,
		"eu-north-1",
		&state.EKSOwnershipState{
			Version:     state.EKSOwnershipStateVersion,
			ClusterName: clusterName,
			Region:      "eu-north-1",
			AccountID:   accountID,
			ClusterARN:  "arn:aws:eks:eu-north-1:" + accountID + ":cluster/" + clusterName,
			CreatedAt:   createdAt,
			AWSOptions:  captureAWSOptions(),
		},
	))
}

func eksCluster(name, arn string, createdAt time.Time) *ekstypes.Cluster {
	return &ekstypes.Cluster{
		Name:      aws.String(name),
		Arn:       aws.String(arn),
		CreatedAt: aws.Time(createdAt),
	}
}

func identityTime() time.Time {
	return time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
}

package cluster_test

import (
	"fmt"
	"testing"

	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEKSAccountID = "123456789012"

// runInterruptedEKSCreate runs a failing create while AWS does not report the cluster yet, the way an
// interrupted create looks before the control plane is visible, and returns the shared identity
// client so the caller can make the cluster appear afterwards.
func runInterruptedEKSCreate(t *testing.T) (*fakeEKSIdentityClient, *[]string) {
	t.Helper()

	client := &fakeEKSIdentityClient{
		accountID:   testEKSAccountID,
		describeErr: fmt.Errorf("%w: st-eks", eksclient.ErrClusterNotFound),
	}

	output, deleted, err := runFailingEKSCreate(t, client)

	require.ErrorIs(t, err, errNodegroupCreateQuota)
	assert.Contains(t, output, "kept this create attempt's identifier")
	assert.NotContains(t, output, "recorded ownership")

	hint, hintErr := state.LoadEKSCreationHint("st-eks", "eu-west-1")
	require.NoError(t, hintErr, "the failed create must keep its attempt identifier")
	assert.Equal(t, []string{testEKSCreationID}, hint.CreationIDs)
	assert.Equal(t, testEKSAccountID, hint.AccountID)

	_, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
	require.ErrorIs(t, loadErr, state.ErrEKSOwnershipStateNotFound,
		"the hint alone must not become an ownership record")

	return client, deleted
}

// appear makes the cluster visible to the identity client, as AWS reports it once creation went on
// after KSail exited.
func (f *fakeEKSIdentityClient) appear(cluster *ekstypes.Cluster) {
	f.describeErr = nil
	f.cluster = cluster
}

// TestCreate_EKSInterruptedCreateLetsDeleteRemoveLateCluster pins ksail#8324: a create interrupted
// before AWS reports the cluster keeps the attempt's identifier, and a later delete records ownership
// and removes the cluster once it appears carrying that identifier's tag. The hint is then spent.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSInterruptedCreateLetsDeleteRemoveLateCluster(t *testing.T) {
	client, deleted := runInterruptedEKSCreate(t)

	client.appear(taggedEKSCluster(testEKSCreationID))

	calls, deleteErr := runEKSDeleteAfterFailedCreate(t)
	require.NoError(t, deleteErr)
	assert.Equal(
		t,
		[]string{"get cluster --name st-eks --output json --region eu-west-1"},
		calls,
		"delete must still pass the eksctl provenance query",
	)
	assert.Equal(t, []string{"st-eks"}, *deleted)

	_, hintErr := state.LoadEKSCreationHint("st-eks", "eu-west-1")
	require.ErrorIs(t, hintErr, state.ErrEKSCreationHintNotFound, "a used hint must be removed")
}

// TestCreate_EKSCreationHintNeverAuthorizesAnotherCluster keeps every other same-named cluster
// refused after an interrupted create: one without this attempt's tag, one carrying another
// attempt's tag, and one reached under another AWS account. The hint stays for the real cluster.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSCreationHintNeverAuthorizesAnotherCluster(t *testing.T) {
	testCases := []struct {
		name      string
		cluster   *ekstypes.Cluster
		accountID string
	}{
		{name: "late cluster without a creation tag", cluster: taggedEKSCluster("")},
		{
			name:    "late cluster created by another attempt",
			cluster: taggedEKSCluster("another-attempt"),
		},
		{
			name:      "caller in another account",
			cluster:   taggedEKSCluster(testEKSCreationID),
			accountID: "210987654321",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client, deleted := runInterruptedEKSCreate(t)

			client.appear(testCase.cluster)

			if testCase.accountID != "" {
				client.accountID = testCase.accountID
			}

			_, deleteErr := runEKSDeleteAfterFailedCreate(t)
			require.Error(t, deleteErr, "delete must refuse a cluster this attempt did not create")
			assert.Empty(t, *deleted)

			_, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
			require.ErrorIs(t, loadErr, state.ErrEKSOwnershipStateNotFound)

			_, hintErr := state.LoadEKSCreationHint("st-eks", "eu-west-1")
			require.NoError(t, hintErr, "an unmatched cluster must not spend the hint")
		})
	}
}

// TestCreate_EKSStaleCreationHintIsRemoved removes the hint once delete confirms that no such
// cluster exists, whether the exact eksctl query or the EKS API reports the absence. Delete still
// refuses, as it did before the hint existed.
func TestCreate_EKSStaleCreationHintIsRemoved(t *testing.T) {
	testCases := []struct {
		name         string
		eksctlAbsent bool
	}{
		{name: "eksctl reports no cluster", eksctlAbsent: true},
		{name: "EKS API reports no cluster"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, deleted := runInterruptedEKSCreate(t)

			if testCase.eksctlAbsent {
				t.Setenv("KSAIL_EKSCTL_CLUSTER_ABSENT", "1")
			}

			_, deleteErr := runEKSDeleteAfterFailedCreate(t)
			require.Error(t, deleteErr)
			assert.Empty(t, *deleted)

			_, hintErr := state.LoadEKSCreationHint("st-eks", "eu-west-1")
			require.ErrorIs(t, hintErr, state.ErrEKSCreationHintNotFound,
				"a hint for a cluster that does not exist must be removed")
		})
	}
}

// notFoundEKSCluster is the identity client's answer while AWS does not report st-eks.
func notFoundEKSCluster() error {
	return fmt.Errorf("%w: st-eks", eksclient.ErrClusterNotFound)
}

// TestCreate_EKSInterruptedCreateKeepsHintWhenLaterAccountLookupFails pins a review finding on
// ksail#8335: the account read before the cluster lookup is reused for the hint, so a second account
// lookup that would fail cannot cost the attempt its hint.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSInterruptedCreateKeepsHintWhenLaterAccountLookupFails(t *testing.T) {
	client := &fakeEKSIdentityClient{
		accountID:       testEKSAccountID,
		describeErr:     notFoundEKSCluster(),
		accountErrAfter: 1,
	}

	output, _, err := runFailingEKSCreate(t, client)

	require.ErrorIs(t, err, errNodegroupCreateQuota)
	require.NotErrorIs(t, err, errEKSIdentityQuery)
	assert.Contains(t, output, "kept this create attempt's identifier")

	hint, hintErr := state.LoadEKSCreationHint("st-eks", "eu-west-1")
	require.NoError(t, hintErr)
	assert.Equal(t, []string{testEKSCreationID}, hint.CreationIDs)
}

// TestCreate_EKSRetriedCreateKeepsEarlierAttempt pins a review finding on ksail#8335: a retry that
// also fails before AWS reports the cluster keeps the earlier attempt, so the cluster the earlier
// attempt started is still deletable when it appears.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSRetriedCreateKeepsEarlierAttempt(t *testing.T) {
	client, _ := runInterruptedEKSCreate(t)

	_, deleted, err := runFailingEKSCreateAttempt(t, client, "attempt-retry")
	require.ErrorIs(t, err, errNodegroupCreateQuota)

	hint, hintErr := state.LoadEKSCreationHint("st-eks", "eu-west-1")
	require.NoError(t, hintErr)
	assert.Equal(t, []string{testEKSCreationID, "attempt-retry"}, hint.CreationIDs)

	client.appear(taggedEKSCluster(testEKSCreationID))

	_, deleteErr := runEKSDeleteAfterFailedCreate(t)
	require.NoError(t, deleteErr)
	assert.Equal(t, []string{"st-eks"}, *deleted)
}

// TestCreate_EKSRetryRecordsEarlierAttemptsLateCluster pins a review finding on ksail#8335: when the
// cluster an earlier interrupted attempt started has appeared, a retry fails because the name is
// taken. That failure records ownership of the cluster, because its live creation tag matches the
// kept earlier attempt, and spends the hint.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSRetryRecordsEarlierAttemptsLateCluster(t *testing.T) {
	client, _ := runInterruptedEKSCreate(t)

	client.appear(taggedEKSCluster(testEKSCreationID))

	output, _, err := runFailingEKSCreateAttempt(t, client, "attempt-retry")
	require.ErrorIs(t, err, errNodegroupCreateQuota)
	assert.Contains(t, output, "recorded ownership of partially created EKS cluster")

	ownership, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
	require.NoError(t, loadErr)
	assert.Equal(t, testEKSAccountID, ownership.AccountID)

	_, hintErr := state.LoadEKSCreationHint("st-eks", "eu-west-1")
	require.ErrorIs(t, hintErr, state.ErrEKSCreationHintNotFound, "the used hint is spent")
}

// TestCreate_EKSHintAloneLetsDeleteReachVerificationOutsideTheProject pins a review finding on
// ksail#8335: when the hint is the only local record of the cluster, a delete by name from another
// directory still reaches the eksctl, account and creation-tag checks, and removes the cluster that
// carries the kept tag.
func TestCreate_EKSHintAloneLetsDeleteReachVerificationOutsideTheProject(t *testing.T) {
	client, deleted := runInterruptedEKSCreate(t)

	client.appear(taggedEKSCluster(testEKSCreationID))
	t.Chdir(t.TempDir())
	t.Setenv("AWS_REGION", "eu-west-1")

	calls, deleteErr := runEKSDeleteAfterFailedCreate(t, "--name", "st-eks", "--provider", "AWS")
	require.NoError(t, deleteErr)
	assert.Contains(t, calls, "get cluster --name st-eks --output json --region eu-west-1")
	assert.Equal(t, []string{"st-eks"}, *deleted)
}

package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const testEKSCreationID = "attempt-7591"

var errNodegroupCreateQuota = errors.New("nodegroup creation exceeded the account quota")

// failingCreateProvisioner models eksctl failing after the call was made, for example when the
// control plane exists but the worker nodegroup cannot be created. It records every cluster later
// deleted through it.
type failingCreateProvisioner struct {
	fakeProvisioner

	deleted *[]string
}

// Create fails the way eksctl does when the worker nodegroup cannot be created.
func (*failingCreateProvisioner) Create(context.Context, string) error {
	return fmt.Errorf("eksctl create cluster: %w", errNodegroupCreateQuota)
}

// Delete records the deleted cluster name.
func (p *failingCreateProvisioner) Delete(_ context.Context, name string) error {
	*p.deleted = append(*p.deleted, name)

	return nil
}

// failingCreateFactory hands out failingCreateProvisioner instances sharing one delete log.
type failingCreateFactory struct{ deleted *[]string }

// Create returns a provisioner whose create fails.
func (f failingCreateFactory) Create(
	context.Context,
	*v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	provisioner := &failingCreateProvisioner{
		createName: nil,
		deleted:    f.deleted,
	}

	return provisioner, &v1alpha4.Cluster{Name: "test"}, nil
}

// taggedEKSCluster is the st-eks cluster as AWS reports it, carrying creationID as its KSail
// creation tag (none when empty).
func taggedEKSCluster(creationID string) *ekstypes.Cluster {
	cluster := immutableEKSClusterInRegion("st-eks", "eu-west-1", immutableIdentityTime())
	if creationID != "" {
		cluster.Tags = map[string]string{eksprovisioner.ClusterCreationTag: creationID}
	}

	return cluster
}

// runFailingEKSCreate runs `ksail cluster create` for the st-eks fixture with a provisioner whose
// create fails. The returned slice collects every cluster a later command deletes through that
// provisioner.
func runFailingEKSCreate(t *testing.T, client eksidentity.Client) (string, *[]string, error) {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupMockRegistryBackend(t)
	writeEKSCreateTestFiles(t, workingDir)
	setupGitOpsTestMocks(t, v1alpha1.GitOpsEngineArgoCD)

	deleted := &[]string{}
	t.Cleanup(cluster.SetProvisionerFactoryForTests(failingCreateFactory{deleted: deleted}))
	t.Cleanup(cluster.ExportSetEKSCreationIDGenerator(func() string { return testEKSCreationID }))
	setEKSIdentityClient(t, client)

	clearFailedCreateOwnership(t)
	t.Cleanup(func() { clearFailedCreateOwnership(t) })

	cmd := cluster.NewCreateCmd()

	var output bytes.Buffer

	cmd.SetContext(t.Context())
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--gitops-engine", "ArgoCD"})

	err := cmd.Execute()

	return output.String(), deleted, err
}

// runEKSDeleteAfterFailedCreate runs `ksail cluster delete --force` in the failed create's working
// directory, with an eksctl stand-in that reports the cluster as eksctl-created and records each
// ownership query. It returns the recorded eksctl calls and the command error.
func runEKSDeleteAfterFailedCreate(t *testing.T) ([]string, error) {
	t.Helper()

	binDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "eksctl-calls")
	writeExecutableFixture(t, filepath.Join(binDir, "eksctl"), standaloneEKSEksctlFixture)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KSAIL_EKSCTL_MARKER", markerPath)
	t.Setenv("KSAIL_EKS_CLUSTER", "st-eks")
	t.Setenv("KSAIL_EKS_DISCOVERED_REGION", "eu-west-1")

	cmd := cluster.NewDeleteCmd()
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--force"})

	err := cmd.Execute()

	return readStandaloneEKSCalls(t, markerPath), err
}

// clearFailedCreateOwnership removes any st-eks state another test left in the shared test home.
func clearFailedCreateOwnership(t *testing.T) {
	t.Helper()
	require.NoError(t, state.DeleteClusterState("st-eks"))
}

// TestCreate_EKSFailedAfterControlPlaneRecordsOwnership pins ksail#7591: a create that fails once
// AWS holds a control plane carrying this attempt's creation tag records the immutable ownership
// identity, so `ksail cluster delete` can verify and remove that exact cluster. The original create
// failure stays visible.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSFailedAfterControlPlaneRecordsOwnership(t *testing.T) {
	client := &fakeEKSIdentityClient{
		accountID: "123456789012",
		cluster:   taggedEKSCluster(testEKSCreationID),
	}

	output, deleted, err := runFailingEKSCreate(t, client)

	require.ErrorIs(t, err, errNodegroupCreateQuota)
	assert.Contains(t, output, "recorded ownership of partially created EKS cluster")

	ownership, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
	require.NoError(t, loadErr)
	assert.Equal(t, "123456789012", ownership.AccountID)
	assert.Equal(t, "arn:aws:eks:eu-west-1:123456789012:cluster/st-eks", ownership.ClusterARN)
	assert.Equal(t, immutableIdentityTime(), ownership.CreatedAt)
	assert.Equal(
		t,
		"AWS_ACCESS_KEY_ID",
		ownership.AWSOptions.AccessKeyIDEnvVar,
		"the record must carry the credential mapping delete resolves later",
	)

	calls, deleteErr := runEKSDeleteAfterFailedCreate(t)
	require.NoError(t, deleteErr)
	assert.Equal(
		t,
		[]string{"get cluster --name st-eks --output json --region eu-west-1"},
		calls,
		"delete must pass the eksctl provenance query",
	)
	assert.Equal(t, []string{"st-eks"}, *deleted)
}

// TestCreate_EKSFailedCreateRetainsNothingWithoutAttributableCluster covers the cases in which a
// failed create must not record ownership, and the following delete must still refuse: nothing was
// created, or the same-named cluster AWS reports does not carry this attempt's creation tag (it
// existed before, or another actor created it).
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSFailedCreateRetainsNothingWithoutAttributableCluster(t *testing.T) {
	testCases := []struct {
		name        string
		cluster     *ekstypes.Cluster
		describeErr error
	}{
		{
			name:        "failure before any control plane exists",
			describeErr: fmt.Errorf("%w: st-eks", eksclient.ErrClusterNotFound),
		},
		{
			name:    "same-named cluster without a creation tag",
			cluster: taggedEKSCluster(""),
		},
		{
			name:    "same-named cluster created by another attempt",
			cluster: taggedEKSCluster("another-attempt"),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &fakeEKSIdentityClient{
				accountID:   "123456789012",
				cluster:     testCase.cluster,
				describeErr: testCase.describeErr,
			}

			output, deleted, err := runFailingEKSCreate(t, client)

			require.ErrorIs(t, err, errNodegroupCreateQuota)
			require.NotErrorIs(t, err, eksidentity.ErrCreationUnattributed)
			assert.NotContains(t, output, "recorded ownership")

			_, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
			require.ErrorIs(t, loadErr, state.ErrEKSOwnershipStateNotFound)

			_, deleteErr := runEKSDeleteAfterFailedCreate(t)
			require.Error(t, deleteErr, "delete must refuse a cluster this attempt did not create")
			assert.Empty(t, *deleted)
		})
	}
}

// TestCreate_EKSFailedCreateReportsOwnershipRecordFailure keeps both failures visible when the
// cluster this attempt created cannot be bound: here AWS reports it under another account.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSFailedCreateReportsOwnershipRecordFailure(t *testing.T) {
	client := &fakeEKSIdentityClient{
		accountID: "210987654321",
		cluster:   taggedEKSCluster(testEKSCreationID),
	}

	_, _, err := runFailingEKSCreate(t, client)

	require.ErrorIs(t, err, errNodegroupCreateQuota)
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	require.ErrorContains(t, err, "record ownership of partially created EKS cluster")

	_, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
	require.ErrorIs(t, loadErr, state.ErrEKSOwnershipStateNotFound)
}

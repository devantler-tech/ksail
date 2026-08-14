package cluster_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errEKSIdentityQuery = errors.New("EKS identity query failed")

type fakeEKSIdentityClient struct {
	accountID      string
	accountErr     error
	cluster        *ekstypes.Cluster
	clusters       []*ekstypes.Cluster
	describeErr    error
	describeCalls  int
	beforeDescribe func(int)
}

func (f *fakeEKSIdentityClient) CallerAccountID(_ context.Context) (string, error) {
	return f.accountID, f.accountErr
}

func (f *fakeEKSIdentityClient) DescribeCluster(
	_ context.Context,
	_ string,
) (*ekstypes.Cluster, error) {
	if f.beforeDescribe != nil {
		f.beforeDescribe(f.describeCalls)
	}

	if len(f.clusters) > 0 {
		index := min(f.describeCalls, len(f.clusters)-1)
		f.describeCalls++

		return f.clusters[index], f.describeErr
	}

	f.describeCalls++

	return f.cluster, f.describeErr
}

//nolint:paralleltest // subtests mutate process environment, working directory, and shared hooks.
func TestStandaloneEKSLifecycleRejectsImmutableIdentityMismatchBeforeMutation(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		//nolint:paralleltest // each case mutates process environment, working directory, and shared hooks.
		t.Run(testCase.name+" account mismatch", func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-account-identity-6202"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			configureStandaloneEKSNodegroupAction(t, testCase.name)
			persistStandaloneEKSIdentity(t, clusterName, immutableIdentityTime())
			setEKSIdentityClient(t, &fakeEKSIdentityClient{accountID: "210987654321"})

			cmd := testCase.newCommand()
			args := make([]string, 0, 4+len(testCase.extraArgs))
			args = append(args, "--name", clusterName, "--provider", "AWS")
			args = append(args, testCase.extraArgs...)
			cmd.SetArgs(args)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
			assert.Equal(t, []string{fmt.Sprintf(
				"get cluster --name %s --output json --region ap-southeast-2",
				clusterName,
			)}, readStandaloneEKSCalls(t, markerPath))
			assertParentAWSEnvironmentUnchanged(t)
		})
	}
}

//nolint:paralleltest // subtests mutate process environment, working directory, and shared hooks.
func TestStandaloneEKSLifecycleRechecksSameARNReplacementImmediatelyBeforeMutation(t *testing.T) {
	for _, testCase := range standaloneEKSLifecycleCases() {
		//nolint:paralleltest // each case mutates process environment, working directory, and shared hooks.
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-" + testCase.name + "-replacement-identity-6202"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			configureStandaloneEKSNodegroupAction(t, testCase.name)

			replacementTime := immutableIdentityTime().Add(time.Minute)

			identityClient := &fakeEKSIdentityClient{
				accountID: "123456789012",
				clusters: []*ekstypes.Cluster{
					immutableEKSCluster(clusterName, immutableIdentityTime()),
					immutableEKSCluster(clusterName, immutableIdentityTime()),
					immutableEKSCluster(clusterName, replacementTime),
				},
				beforeDescribe: func(call int) {
					if call == 2 {
						persistStandaloneEKSIdentity(
							t, clusterName, replacementTime,
						)
					}
				},
			}
			setEKSIdentityClient(t, identityClient)

			cmd := testCase.newCommand()
			args := make([]string, 0, 4+len(testCase.extraArgs))
			args = append(args, "--name", clusterName, "--provider", "AWS")
			args = append(args, testCase.extraArgs...)
			cmd.SetArgs(args)
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
			require.ErrorContains(t, err, "creation time")

			expectedCalls := testCase.expectedCalls(clusterName)
			assert.Equal(
				t,
				expectedCalls[:len(expectedCalls)-1],
				readStandaloneEKSCalls(t, markerPath),
			)
			assert.Equal(t, 3, identityClient.describeCalls)
		})
	}
}

// setDefaultAWSOptionsOnOwnership stamps the canonical mapping onto an already-persisted ownership
// record, mirroring what creation now captures.
func setDefaultAWSOptionsOnOwnership(t *testing.T, clusterName, region string) {
	t.Helper()

	ownership, err := state.LoadEKSOwnershipState(clusterName, region)
	require.NoError(t, err)

	ownership.AWSOptions = credentials.AWSOptionsWithDefaults(v1alpha1.OptionsAWS{})

	require.NoError(t, state.SaveEKSOwnershipState(clusterName, region, ownership))
}

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestStandaloneEKSLifecycleFreezesCustomAWSCredentialsForEveryConsumer(t *testing.T) {
	const clusterName = "ksail-eks-start-frozen-credentials-6202"

	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	setDefaultAWSOptionsOnOwnership(t, clusterName, "ap-southeast-2")

	identityClient := &fakeEKSIdentityClient{
		accountID: "123456789012",
		cluster: immutableEKSCluster(
			clusterName,
			immutableIdentityTime(),
		),
	}

	var (
		captured       credentials.AWSResolution
		capturedRegion string
		factoryCalls   int
	)

	setEKSIdentityClientWithResolutionObserver(
		t,
		identityClient,
		func(region string, resolution credentials.AWSResolution) {
			factoryCalls++
			capturedRegion = region
			captured = resolution
		},
	)

	cmd := cluster.NewStartCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	assert.True(t, captured.IsFrozen())
	assert.Empty(t, captured.Profile)
	assert.Equal(t, "fixture-access", captured.AccessKeyID)
	assert.Equal(t, "fixture-secret", captured.SecretAccessKey)
	assert.Equal(t, "fixture-session", captured.SessionToken)
	assert.Equal(t, "ap-southeast-2", capturedRegion)
	assert.Equal(t, 1, factoryCalls)
	assert.Equal(t, 3, identityClient.describeCalls)
	assert.Equal(
		t,
		standaloneEKSLifecycleCases()[1].expectedCalls(clusterName),
		readStandaloneEKSCalls(t, markerPath),
	)
	assert.Equal(t, "selected-profile", os.Getenv("KSAIL_PROFILE"))
	assert.Equal(t, "fixture-access", os.Getenv("KSAIL_ACCESS"))
	assert.Equal(t, "fixture-secret", os.Getenv("KSAIL_SECRET"))
	assert.Equal(t, "fixture-session", os.Getenv("KSAIL_SESSION"))
	assertParentAWSEnvironmentUnchanged(t)
}

//nolint:paralleltest // table mutates process environment, working directory, and shared hooks.
func TestStandaloneEKSLifecycleIdentityQueryFailuresNeverReachMutation(t *testing.T) {
	queryCases := []struct {
		name   string
		client func() *fakeEKSIdentityClient
	}{
		{
			name: "caller account",
			client: func() *fakeEKSIdentityClient {
				return &fakeEKSIdentityClient{accountErr: errEKSIdentityQuery}
			},
		},
		{
			name: "cluster description",
			client: func() *fakeEKSIdentityClient {
				return &fakeEKSIdentityClient{
					accountID:   "123456789012",
					describeErr: errEKSIdentityQuery,
				}
			},
		},
	}

	for _, lifecycleCase := range standaloneEKSLifecycleCases() {
		for _, queryCase := range queryCases {
			t.Run(lifecycleCase.name+" "+queryCase.name, func(t *testing.T) {
				clusterName := "ksail-eks-" + lifecycleCase.name + "-query-error-6202"
				markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
				configureStandaloneEKSNodegroupAction(t, lifecycleCase.name)
				setEKSIdentityClient(t, queryCase.client())

				cmd := lifecycleCase.newCommand()
				args := []string{"--name", clusterName, "--provider", "AWS"}
				args = append(args, lifecycleCase.extraArgs...)
				cmd.SetArgs(args)
				cmd.SetContext(t.Context())
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)

				err := cmd.Execute()
				require.ErrorIs(t, err, errEKSIdentityQuery)
				assert.Equal(t, []string{fmt.Sprintf(
					"get cluster --name %s --output json --region ap-southeast-2",
					clusterName,
				)}, readStandaloneEKSCalls(t, markerPath))
				assertParentAWSEnvironmentUnchanged(t)
			})
		}
	}
}

//nolint:paralleltest // subtests mutate process environment, working directory, and shared hooks.
func TestStandaloneEKSStartRejectsLegacyAndMalformedOwnershipState(t *testing.T) {
	testCases := []struct {
		name      string
		slug      string
		corrupt   func(t *testing.T, clusterName string)
		wantCause error
	}{
		{
			name: "legacy missing identity",
			slug: "legacy-missing",
			corrupt: func(t *testing.T, clusterName string) {
				t.Helper()
				require.NoError(t, state.DeleteClusterState(clusterName))
			},
			wantCause: state.ErrEKSOwnershipStateNotFound,
		},
		{
			name: "malformed identity",
			slug: "malformed",
			corrupt: func(t *testing.T, clusterName string) {
				t.Helper()

				home, err := os.UserHomeDir()
				require.NoError(t, err)

				path := filepath.Join(
					home, ".ksail", "clusters", clusterName,
					"eks-ownership-ap-southeast-2.json",
				)
				require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
			},
			wantCause: state.ErrInvalidEKSOwnershipState,
		},
	}

	for _, testCase := range testCases {
		//nolint:paralleltest // each case mutates process environment, working directory, and shared hooks.
		t.Run(testCase.name, func(t *testing.T) {
			clusterName := "ksail-eks-start-" + testCase.slug + "-6202"
			markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
			testCase.corrupt(t, clusterName)

			cmd := cluster.NewStartCmd()
			cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
			cmd.SetContext(t.Context())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.ErrorIs(t, err, testCase.wantCause)
			require.ErrorContains(t, err, "eks-bind")
			assert.Equal(t, []string{fmt.Sprintf(
				"get cluster --name %s --output json --region ap-southeast-2",
				clusterName,
			)}, readStandaloneEKSCalls(t, markerPath))
		})
	}
}

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestEKSUpdateRejectsImmutableIdentityMismatchBeforePlanning(t *testing.T) {
	clusterName := "ksail-eks-update-account-identity-6202"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	setEKSIdentityClient(t, &fakeEKSIdentityClient{accountID: "210987654321"})

	cmd := cluster.NewUpdateCmd()
	cmd.SetArgs([]string{})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	assert.Equal(t, []string{fmt.Sprintf(
		"get cluster --name %s --output json --region ap-southeast-2",
		clusterName,
	)}, readStandaloneEKSCalls(t, markerPath))
}

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestEKSTTLCleanupRejectsImmutableIdentityMismatchBeforeDelete(t *testing.T) {
	clusterName := "ksail-eks-ttl-account-identity-6202"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	setEKSIdentityClient(t, &fakeEKSIdentityClient{accountID: "210987654321"})

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cluster.ExportAutoDeleteCluster(
		cmd,
		clusterName,
		standaloneEKSTTLClusterConfig(),
		&clusterprovisioner.EKSConfig{
			Name:           clusterName,
			Region:         "ap-southeast-2",
			ConfigPath:     filepath.Join(".", "eks.yaml"),
			KubeconfigPath: filepath.Join(".", "kubeconfig"),
			NameFromConfig: true,
		},
	)
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	assert.Equal(t, []string{fmt.Sprintf(
		"get cluster --name %s --output json --region ap-southeast-2",
		clusterName,
	)}, readStandaloneEKSCalls(t, markerPath))
}

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestEKSTTLCleanupRechecksSameARNReplacementImmediatelyBeforeDelete(t *testing.T) {
	clusterName := "ksail-eks-ttl-replacement-identity-6202"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	identityClient := &fakeEKSIdentityClient{
		accountID: "123456789012",
		clusters: []*ekstypes.Cluster{
			immutableEKSCluster(clusterName, immutableIdentityTime()),
			immutableEKSCluster(clusterName, immutableIdentityTime()),
			immutableEKSCluster(
				clusterName,
				immutableIdentityTime().Add(time.Minute),
			),
		},
	}
	setEKSIdentityClient(t, identityClient)

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cluster.ExportAutoDeleteCluster(
		cmd,
		clusterName,
		standaloneEKSTTLClusterConfig(),
		&clusterprovisioner.EKSConfig{
			Name:           clusterName,
			Region:         "ap-southeast-2",
			ConfigPath:     filepath.Join(".", "eks.yaml"),
			KubeconfigPath: filepath.Join(".", "kubeconfig"),
			NameFromConfig: true,
		},
	)
	require.ErrorIs(t, err, eksidentity.ErrIdentityMismatch)
	assert.Equal(t, []string{fmt.Sprintf(
		"get cluster --name %s --output json --region ap-southeast-2",
		clusterName,
	)}, readStandaloneEKSCalls(t, markerPath))
	assert.Equal(t, 3, identityClient.describeCalls)
}

func setEKSIdentityClient(t *testing.T, client eksidentity.Client) {
	t.Helper()
	setEKSIdentityClientWithResolutionObserver(t, client, nil)
}

func setEKSIdentityClientWithResolutionObserver(
	t *testing.T,
	client eksidentity.Client,
	observe func(string, credentials.AWSResolution),
) {
	t.Helper()

	restore := cluster.ExportSetEKSIdentityClientFactory(
		func(
			_ context.Context,
			region string,
			resolution credentials.AWSResolution,
		) (eksidentity.Client, error) {
			if observe != nil {
				observe(region, resolution)
			}

			return client, nil
		},
	)
	t.Cleanup(restore)
}

func persistStandaloneEKSIdentity(
	t *testing.T,
	clusterName string,
	createdAt time.Time,
) {
	t.Helper()

	persistStandaloneEKSIdentityInRegion(
		t,
		clusterName,
		"ap-southeast-2",
		"123456789012",
		createdAt,
	)
}

func persistStandaloneEKSIdentityInRegion(
	t *testing.T,
	clusterName, region, accountID string,
	createdAt time.Time,
) {
	t.Helper()

	require.NoError(t, state.SaveEKSOwnershipState(
		clusterName,
		region,
		&state.EKSOwnershipState{
			Version:     state.EKSOwnershipStateVersion,
			ClusterName: clusterName,
			Region:      region,
			AccountID:   accountID,
			ClusterARN:  "arn:aws:eks:" + region + ":" + accountID + ":cluster/" + clusterName,
			CreatedAt:   createdAt,
			AWSOptions:  credentials.AWSOptionsWithDefaults(v1alpha1.OptionsAWS{}),
		},
	))
}

func configureStandaloneEKSIdentityInRegion(
	t *testing.T,
	clusterName, region string,
) {
	t.Helper()

	persistStandaloneEKSIdentityInRegion(
		t,
		clusterName,
		region,
		"123456789012",
		immutableIdentityTime(),
	)
	setEKSIdentityClient(t, &fakeEKSIdentityClient{
		accountID: "123456789012",
		cluster: immutableEKSClusterInRegion(
			clusterName,
			region,
			immutableIdentityTime(),
		),
	})
}

func immutableEKSCluster(
	clusterName string,
	createdAt time.Time,
) *ekstypes.Cluster {
	return immutableEKSClusterInRegion(
		clusterName, "ap-southeast-2", createdAt,
	)
}

func immutableEKSClusterInRegion(
	clusterName, region string,
	createdAt time.Time,
) *ekstypes.Cluster {
	return &ekstypes.Cluster{
		Name: aws.String(clusterName),
		Arn: aws.String(
			"arn:aws:eks:" + region + ":123456789012:cluster/" + clusterName,
		),
		CreatedAt: aws.Time(createdAt),
	}
}

func immutableIdentityTime() time.Time {
	return time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
}

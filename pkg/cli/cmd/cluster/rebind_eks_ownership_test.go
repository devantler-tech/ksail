package cluster_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestRebindEKSOwnershipIsOffByDefault(t *testing.T) {
	clusterName := "eks-rebind-disabled-6202"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	require.NoError(t, state.DeleteClusterState(clusterName))

	cmd := cluster.NewRebindEKSOwnershipCmd()
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, experimental.ErrDisabled)
	assert.Empty(t, readStandaloneEKSCalls(t, markerPath))
}

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestRebindEKSOwnershipPrintsIdentityBeforeConfirmation(t *testing.T) {
	clusterName := "eks-rebind-review-6202"
	setupStandaloneEKSLifecycleFixture(t, clusterName)
	require.NoError(t, state.DeleteClusterState(clusterName))

	cmd := cluster.NewRebindEKSOwnershipCmd()
	cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS"})
	cmd.SetContext(t.Context())

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.Execute()
	require.ErrorContains(t, err, "--yes")
	assert.Contains(t, output.String(), "123456789012")
	assert.Contains(
		t,
		output.String(),
		"arn:aws:eks:ap-southeast-2:123456789012:cluster/"+clusterName,
	)
	assert.Contains(t, output.String(), immutableIdentityTime().Format("2006-01-02T15:04:05Z07:00"))

	_, loadErr := state.LoadEKSOwnershipState(clusterName, "ap-southeast-2")
	require.ErrorIs(t, loadErr, state.ErrEKSOwnershipStateNotFound)
}

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestRebindEKSOwnershipPersistsOnlyAfterExplicitConfirmation(t *testing.T) {
	clusterName := "eks-rebind-confirmed-6202"
	markerPath := setupStandaloneEKSLifecycleFixture(t, clusterName)
	require.NoError(t, state.DeleteClusterState(clusterName))
	require.NoError(t, state.SaveEKSNodegroupState(
		clusterName,
		"ap-southeast-2",
		&state.EKSNodegroupState{
			Version:     state.EKSNodegroupStateVersion,
			ClusterName: clusterName,
			Region:      "ap-southeast-2",
			Nodegroups: []state.EKSNodegroupCapacity{{
				Name:            "workers",
				DesiredCapacity: 3,
				MinSize:         1,
				MaxSize:         5,
			}},
		},
	))

	cmd := cluster.NewRebindEKSOwnershipCmd()
	cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")
	cmd.SetArgs([]string{"--name", clusterName, "--provider", "AWS", "--yes"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	ownership, err := state.LoadEKSOwnershipState(clusterName, "ap-southeast-2")
	require.NoError(t, err)
	assert.Equal(t, "123456789012", ownership.AccountID)
	assert.Equal(t, immutableIdentityTime(), ownership.CreatedAt)

	_, err = state.LoadEKSNodegroupState(clusterName, "ap-southeast-2")
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)

	for _, call := range readStandaloneEKSCalls(t, markerPath) {
		assert.NotContains(t, call, "delete cluster")
		assert.NotContains(t, call, "scale nodegroup")
	}
}

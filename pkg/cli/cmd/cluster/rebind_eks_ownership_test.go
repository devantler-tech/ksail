package cluster_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestRebindEKSOwnershipIsOffByDefault(t *testing.T) {
	clusterName := "eks-rebind-disabled-6202"
	markerPath, _ := setupStandaloneEKSLifecycleFixture(t, clusterName)
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

// TestRebindEKSOwnershipRecoversWithoutProjectOrCreationState exercises the supported recovery
// command from an empty directory, then proves the recovered record authorizes a guarded stop.
//
//nolint:paralleltest // changes process environment and working directory through the fixture.
func TestRebindEKSOwnershipRecoversWithoutProjectOrCreationState(t *testing.T) {
	const name = "eks-recover-empty-home"

	marker, _ := setupStandaloneEKSLifecycleFixture(t, name)
	removeEKSRecoveryProject(t, name)

	preview := recoveryEKSCommand(t, name, false)

	var output bytes.Buffer
	preview.SetOut(&output)
	require.ErrorContains(t, preview.Execute(), "--yes")
	assert.Contains(t, output.String(), "123456789012")
	assert.Contains(t, output.String(), "ap-southeast-2")

	_, err := state.LoadEKSOwnershipState(name, "ap-southeast-2")
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)

	require.NoError(t, recoveryEKSCommand(t, name, true).Execute())
	_, err = state.LoadClusterSpec(name)
	require.ErrorIs(t, err, state.ErrStateNotFound)
	ownership, err := state.LoadEKSOwnershipState(name, "ap-southeast-2")
	require.NoError(t, err)
	assert.Equal(t, "AWS_ACCESS_KEY_ID", ownership.AWSOptions.AccessKeyIDEnvVar)

	configureStandaloneEKSNodegroupAction(t, "stop")
	runStandaloneEKSCommand(t, cluster.NewStopCmd, "--name", name, "--provider", "AWS")
	assert.Contains(
		t,
		readStandaloneEKSCalls(t, marker),
		"scale nodegroup --cluster "+name+" --name workers --nodes 0 --nodes-min 0 --nodes-max 4 --region ap-southeast-2",
	)
}

// TestReboundEKSRejectsAReplacementCluster proves a recovered record still binds CreatedAt,
// rather than allowing the next incarnation with the same name and ARN to be mutated.
//
//nolint:paralleltest // changes process environment, working directory and SDK hooks.
func TestReboundEKSRejectsAReplacementCluster(t *testing.T) {
	const name = "eks-recovered-replacement"

	marker, _ := setupStandaloneEKSLifecycleFixture(t, name)
	removeEKSRecoveryProject(t, name)
	require.NoError(t, recoveryEKSCommand(t, name, true).Execute())
	setEKSIdentityClient(t, &fakeEKSIdentityClient{
		accountID: "123456789012",
		cluster:   immutableEKSCluster(name, immutableIdentityTime().Add(time.Hour)),
	})

	cmd := cluster.NewStopCmd()
	cmd.SetArgs([]string{"--name", name, "--provider", "AWS"})
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.ErrorContains(t, cmd.Execute(), "identity mismatch")

	for _, call := range readStandaloneEKSCalls(t, marker) {
		assert.NotContains(t, call, "scale nodegroup")
		assert.NotContains(t, call, "delete cluster")
	}
}

// removeEKSRecoveryProject models total local-state loss, retaining only explicit AWS selection.
func removeEKSRecoveryProject(t *testing.T, name string) {
	t.Helper()
	require.NoError(t, state.DeleteClusterState(name))
	require.NoError(t, os.Remove("ksail.yaml"))
	require.NoError(t, os.Remove("eks.yaml"))
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "ap-southeast-2")
	t.Setenv("AWS_ACCESS_KEY_ID", "fixture-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-secret")
	t.Setenv("AWS_SESSION_TOKEN", "fixture-session")

	for _, key := range []string{"KSAIL_PROFILE", "KSAIL_ACCESS", "KSAIL_SECRET", "KSAIL_SESSION"} {
		require.NoError(t, os.Unsetenv(key))
	}
}

// recoveryEKSCommand always names the target and provider explicitly and retains the feature gate.
func recoveryEKSCommand(t *testing.T, name string, confirmed bool) *cobra.Command {
	t.Helper()

	cmd := cluster.NewRebindEKSOwnershipCmd()
	cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")

	args := []string{"--name", name, "--provider", "AWS"}
	if confirmed {
		args = append(args, "--yes")
	}

	cmd.SetArgs(args)
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	return cmd
}

// TestEKSRecoveryRefusesConflictingCreationState prevents a rebind from writing an EKS identity
// into another distribution's state directory.
//
//nolint:paralleltest // each fixture changes the process environment and working directory.
func TestEKSRecoveryRefusesConflictingCreationState(t *testing.T) {
	for _, distribution := range []v1alpha1.Distribution{v1alpha1.DistributionVanilla, v1alpha1.DistributionTalos} {
		t.Run(string(distribution), func(t *testing.T) {
			const name = "recovery-name-collision"

			marker, _ := setupStandaloneEKSLifecycleFixture(t, name)
			removeEKSRecoveryProject(t, name)
			require.NoError(t, state.SaveClusterSpec(name, &v1alpha1.ClusterSpec{
				Distribution: distribution, Provider: v1alpha1.ProviderDocker,
			}))
			require.ErrorContains(t, recoveryEKSCommand(t, name, true).Execute(), "local state")
			_, err := state.LoadEKSOwnershipState(name, "ap-southeast-2")
			require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)
			assert.Empty(t, readStandaloneEKSCalls(t, marker))
		})
	}
}

// TestEKSRecoveryRequiresExplicitTargetWithoutLocalEvidence prevents the recovery command from
// silently adopting whichever cluster happens to be selected in kubeconfig.
func TestEKSRecoveryRequiresExplicitTargetWithoutLocalEvidence(t *testing.T) {
	const name = "eks-inferred-recovery"

	marker, _ := setupStandaloneEKSLifecycleFixture(t, name)
	removeEKSRecoveryProject(t, name)
	writeStandaloneEKSKubeconfigContexts(t, name, []string{
		"operator@" + name + ".ap-southeast-2.eksctl.io",
	})

	path, err := filepath.Abs("kubeconfig")
	require.NoError(t, err)
	t.Setenv("KUBECONFIG", path)
	cmd := recoveryEKSCommand(t, name, true)
	cmd.SetArgs([]string{"--provider", "AWS", "--yes"})
	require.ErrorContains(t, cmd.Execute(), "explicit --name")
	assert.Empty(t, readStandaloneEKSCalls(t, marker))
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
	markerPath, _ := setupStandaloneEKSLifecycleFixture(t, clusterName)
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
	assert.Equal(t, v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		RegionEnvVar:          "KSAIL_REGION",
		AccessKeyIDEnvVar:     "KSAIL_ACCESS",
		SecretAccessKeyEnvVar: "KSAIL_SECRET",
		SessionTokenEnvVar:    "KSAIL_SESSION",
	}, ownership.AWSOptions)

	_, err = state.LoadEKSNodegroupState(clusterName, "ap-southeast-2")
	require.ErrorIs(t, err, state.ErrEKSNodegroupStateNotFound)

	for _, call := range readStandaloneEKSCalls(t, markerPath) {
		assert.NotContains(t, call, "delete cluster")
		assert.NotContains(t, call, "scale nodegroup")
	}
}

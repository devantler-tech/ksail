package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const detailedEKSOwnershipFixture = `#!/bin/sh
printf '%s\n' "$*" >> "$KSAIL_EKSCTL_MARKER"
case "$1 $2 $3" in
  'get cluster --name')
    printf '[{"Name":"%s","Status":"ACTIVE","Version":"1.36"}]\n' "$KSAIL_EKS_CLUSTER"
    ;;
  'get cluster --output')
    printf '[{"Name":"%s","Region":"ap-southeast-2","Owned":"%s"}]\n' \
      "$KSAIL_EKS_CLUSTER" "${KSAIL_EKSCTL_CREATED-True}"
    ;;
  'get nodegroup --cluster')
    printf '[{"Name":"workers","DesiredCapacity":2,"MinSize":2,"MaxSize":4,"Type":"managed"}]\n'
    ;;
  'scale nodegroup --cluster') ;;
  *) exit 51 ;;
esac
`

// This command-level boundary test uses eksctl's distinct named and regional
// JSON shapes. The old parser blocks recovery even for an owned live identity.
//
//nolint:paralleltest // fixture changes process environment, directory and SDK hooks.
func TestEKSRecoveryUsesDetailedNamedAndRegionalOwnedResponses(t *testing.T) {
	const name = "eks-real-wire-recovery"

	marker, binary := setupStandaloneEKSLifecycleFixture(t, name)
	writeExecutableFixture(t, binary, detailedEKSOwnershipFixture)
	removeEKSRecoveryProject(t, name)

	require.ErrorContains(t, recoveryEKSCommand(t, name, false).Execute(), "--yes")
	_, err := state.LoadEKSOwnershipState(name, "ap-southeast-2")
	require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)

	require.NoError(t, recoveryEKSCommand(t, name, true).Execute())
	configureStandaloneEKSNodegroupAction(t, "stop")
	runStandaloneEKSCommand(t, cluster.NewStopCmd, "--name", name, "--provider", "AWS")
	assert.Contains(
		t,
		readStandaloneEKSCalls(t, marker),
		"scale nodegroup --cluster "+name+" --name workers --nodes 0 --nodes-min 0 --nodes-max 4 --region ap-southeast-2",
	)
}

// TestEKSRecoveryRejectsNegativeRegionalOwnership refuses native false, unknown, and absent provenance.
func TestEKSRecoveryRejectsNegativeRegionalOwnership(t *testing.T) {
	for _, owned := range []string{"False", "Unknown", ""} {
		t.Run(owned, func(t *testing.T) {
			const name = "eks-negative-wire-recovery"

			marker, binary := setupStandaloneEKSLifecycleFixture(t, name)
			writeExecutableFixture(t, binary, detailedEKSOwnershipFixture)
			removeEKSRecoveryProject(t, name)
			t.Setenv("KSAIL_EKSCTL_CREATED", owned)

			require.Error(t, recoveryEKSCommand(t, name, true).Execute())
			_, err := state.LoadEKSOwnershipState(name, "ap-southeast-2")
			require.ErrorIs(t, err, state.ErrEKSOwnershipStateNotFound)

			for _, call := range readStandaloneEKSCalls(t, marker) {
				assert.NotContains(t, call, "scale nodegroup")
				assert.NotContains(t, call, "delete cluster")
			}
		})
	}
}

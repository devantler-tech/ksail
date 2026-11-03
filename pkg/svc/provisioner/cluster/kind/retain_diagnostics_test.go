package kindprovisioner_test

import (
	"context"
	"slices"
	"testing"

	cmdrunner "github.com/devantler-tech/ksail/v7/pkg/runner"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

type retainDiagnosticRunner struct{ args []string }

func (r *retainDiagnosticRunner) Run(
	_ context.Context, _ *cobra.Command, args []string,
) (cmdrunner.CommandResult, error) {
	r.args = slices.Clone(args)

	return cmdrunner.CommandResult{}, nil
}

func TestKindRetainsNodesOnlyForOptedInNestedDiagnostics(t *testing.T) {
	t.Parallel()

	for _, retain := range []bool{false, true} {
		runner := &retainDiagnosticRunner{}
		provisioner := kindprovisioner.NewProvisionerWithRunner(
			&v1alpha4.Cluster{}, "", nil, nil, runner,
		)
		provisioner.WithRetainOnFailureForTest(retain)
		require.NoError(t, provisioner.Create(t.Context(), "fixture"))
		require.Equal(t, retain, slices.Contains(runner.args, "--retain"))
	}
}

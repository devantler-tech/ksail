package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenPodDisruptionBudget(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewPodDisruptionBudgetCmd, []string{
		"test-pdb",
		"--min-available=2",
		"--selector=app=test",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

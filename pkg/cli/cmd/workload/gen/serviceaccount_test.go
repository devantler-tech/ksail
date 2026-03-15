package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenServiceAccount(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceAccountCmd, []string{"test-sa"})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

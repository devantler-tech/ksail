package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenJob(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewJobCmd, []string{
		"test-job",
		"--image=busybox:latest",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

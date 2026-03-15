package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenCronJob(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewCronJobCmd, []string{
		"test-cronjob",
		"--image=busybox:latest",
		"--schedule=*/5 * * * *",
		"--restart=OnFailure",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

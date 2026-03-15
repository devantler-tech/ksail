package gen_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func execCronJob(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := gen.NewCronJobCmd(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

func TestGenCronJob(t *testing.T) {
	t.Parallel()

	output, _, err := execCronJob(t, []string{
		"test-cronjob",
		"--image=busybox:latest",
		"--schedule=*/5 * * * *",
		"--restart=OnFailure",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

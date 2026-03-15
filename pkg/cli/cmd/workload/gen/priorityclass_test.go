package gen_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func execPriorityClass(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := gen.NewPriorityClassCmd(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

func TestGenPriorityClass(t *testing.T) {
	t.Parallel()

	output, _, err := execPriorityClass(t, []string{
		"test-priority",
		"--value=1000",
		"--description=Test priority class",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

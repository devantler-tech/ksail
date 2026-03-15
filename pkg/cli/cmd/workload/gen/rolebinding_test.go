package gen_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func execRoleBinding(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := gen.NewRoleBindingCmd(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

func TestGenRoleBinding(t *testing.T) {
	t.Parallel()

	output, _, err := execRoleBinding(t, []string{
		"test-rolebinding",
		"--role=test-role",
		"--user=test-user",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

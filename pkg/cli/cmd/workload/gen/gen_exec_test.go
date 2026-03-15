package gen_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// execGen runs the command returned by cmdFactory with the provided args,
// returning stdout, stderr, and any execution error.
func execGen(t *testing.T, cmdFactory func(*di.Runtime) *cobra.Command, args []string) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := cmdFactory(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

package gen_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func execService(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := gen.NewServiceCmd(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

func TestGenServiceClusterIP(t *testing.T) {
	t.Parallel()

	output, _, err := execService(t, []string{
		"clusterip", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceNodePort(t *testing.T) {
	t.Parallel()

	output, _, err := execService(t, []string{
		"nodeport", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceLoadBalancer(t *testing.T) {
	t.Parallel()

	output, _, err := execService(t, []string{
		"loadbalancer", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceExternalName(t *testing.T) {
	t.Parallel()

	output, _, err := execService(t, []string{
		"externalname", "test-svc",
		"--external-name=example.com",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

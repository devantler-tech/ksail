package gen_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func execIngress(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	rt := di.NewRuntime()
	cmd := gen.NewIngressCmd(rt)

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return outBuf.String(), errBuf.String(), err
}

func TestGenIngressSimple(t *testing.T) {
	t.Parallel()

	output, _, err := execIngress(t, []string{
		"test-ingress",
		"--rule=example.com/*=svc:80",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenIngressWithTLS(t *testing.T) {
	t.Parallel()

	output, _, err := execIngress(t, []string{
		"test-ingress-tls",
		"--rule=secure.example.com/*=svc:443,tls=my-tls-secret",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenIngressMultipleRules(t *testing.T) {
	t.Parallel()

	output, _, err := execIngress(t, []string{
		"test-ingress-multi",
		"--rule=api.example.com/*=api-svc:8080",
		"--rule=web.example.com/*=web-svc:80",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

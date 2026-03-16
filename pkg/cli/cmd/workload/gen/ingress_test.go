package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenIngressSimple(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewIngressCmd, []string{
		"test-ingress",
		"--rule=example.com/*=svc:80",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenIngressWithTLS(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewIngressCmd, []string{
		"test-ingress-tls",
		"--rule=secure.example.com/*=svc:443,tls=my-tls-secret",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenIngressMultipleRules(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewIngressCmd, []string{
		"test-ingress-multi",
		"--rule=api.example.com/*=api-svc:8080",
		"--rule=web.example.com/*=web-svc:80",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

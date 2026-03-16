package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenServiceClusterIP(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"clusterip", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceNodePort(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"nodeport", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceLoadBalancer(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"loadbalancer", "test-svc",
		"--tcp=80:8080",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

func TestGenServiceExternalName(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewServiceCmd, []string{
		"externalname", "test-svc",
		"--external-name=example.com",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

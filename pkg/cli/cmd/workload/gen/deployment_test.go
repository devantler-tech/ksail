package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenDeployment(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewDeploymentCmd, []string{
		"test-deployment",
		"--image=nginx:1.21",
		"--replicas=3",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenConfigMap(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewConfigMapCmd, []string{
		"test-config",
		"--from-literal=APP_ENV=production",
		"--from-literal=DEBUG=false",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

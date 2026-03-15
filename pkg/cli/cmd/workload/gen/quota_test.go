package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenQuota(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewQuotaCmd, []string{
		"test-quota",
		"--hard=cpu=1,memory=1Gi,pods=10",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

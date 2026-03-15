package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenRoleBinding(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewRoleBindingCmd, []string{
		"test-rolebinding",
		"--role=test-role",
		"--user=test-user",
	})

	require.NoError(t, err)
	snaps.MatchSnapshot(t, output)
}

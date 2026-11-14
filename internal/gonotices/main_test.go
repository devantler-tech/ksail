package gonotices_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestMainPrintsTheUnderlyingCauseOfAFailure(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	code := gonotices.Main(t.TempDir(), []string{"unknown-operation"}, &stdout, &stderr)

	require.Equal(t, 1, code)
	require.Empty(t, stdout.String())
	require.Equal(
		t,
		gonotices.FailureMessage+": read command arguments: "+gonotices.ErrIncomplete.Error()+"\n",
		stderr.String(),
	)
}

func TestMainNamesTheFailedStep(t *testing.T) {
	t.Parallel()
	root, release := commandInputFixture(t)

	var stdout, stderr bytes.Buffer

	code := gonotices.Main(
		root,
		[]string{
			"stage",
			release.Target,
			release.Version,
			strings.Repeat("a", len(release.Source)),
		},
		&stdout,
		&stderr,
	)

	require.Equal(t, 1, code)
	require.Equal(
		t,
		gonotices.FailureMessage+": check source checkout: "+gonotices.ErrIncomplete.Error()+"\n",
		stderr.String(),
	)
	require.NotContains(t, stderr.String(), root)
}

func TestMainReportsSuccessWithExitCodeZero(t *testing.T) {
	t.Parallel()
	root, release := commandInputFixture(t)

	var stdout, stderr bytes.Buffer

	code := gonotices.Main(
		root,
		[]string{"stage", release.Target, release.Version, release.Source},
		&stdout,
		&stderr,
	)

	require.Equal(t, 0, code)
	require.Empty(t, stderr.String())
	require.Contains(t, stdout.String(), "Verified Go recipient material for "+release.Target)
}

package project_test

import (
	"bytes"
	"os"
	"testing"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	projectpkg "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestProjectCmd_ShowsHelp(t *testing.T) {
	t.Parallel()

	cmd := projectpkg.NewProjectCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	require.Contains(t, output, "Manage the GitOps project files")

	snaps.MatchSnapshot(t, output)
}

func TestProjectCmd_IsExcludedFromToolSurface(t *testing.T) {
	t.Parallel()

	cmd := projectpkg.NewProjectCmd()
	require.Equal(
		t,
		annotations.AnnotationValueTrue,
		cmd.Annotations[annotations.AnnotationExclude],
	)
}

func TestProjectCmd_RejectsArgs(t *testing.T) {
	t.Parallel()

	cmd := projectpkg.NewProjectCmd()

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unexpected-arg"})

	err := cmd.Execute()
	require.Error(t, err)
}

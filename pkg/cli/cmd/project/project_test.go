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

func TestProjectCmd_ConsolidatedIntoToolSurface(t *testing.T) {
	t.Parallel()

	cmd := projectpkg.NewProjectCmd()

	// Now that the group hosts a subcommand it is consolidated into the tool
	// surface (mirroring the cluster group), no longer excluded.
	require.Equal(t, "command", cmd.Annotations[annotations.AnnotationConsolidate])
	require.Empty(t, cmd.Annotations[annotations.AnnotationExclude])

	var found bool

	for _, sub := range cmd.Commands() {
		if sub.Name() == "add-environment" {
			found = true

			break
		}
	}

	require.True(t, found, "project group should host the add-environment subcommand")
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

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

// TestMain wires the snapshot test runner so obsolete snapshots are cleaned
// and snapshot files are kept sorted.
func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

// TestProjectCmd_ShowsHelp pins that running the bare project group prints its
// help text (the group has no action of its own), snapshotting the output.
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

// TestProjectCmd_ConsolidatedIntoToolSurface pins that the project group
// carries the toolgen consolidate annotation (and no exclude annotation) and
// hosts the env subcommand group, so it joins the generated MCP/chat tool
// surface like the cluster group.
func TestProjectCmd_ConsolidatedIntoToolSurface(t *testing.T) {
	t.Parallel()

	cmd := projectpkg.NewProjectCmd()

	// Now that the group hosts a subcommand it is consolidated into the tool
	// surface (mirroring the cluster group), no longer excluded.
	require.Equal(t, "command", cmd.Annotations[annotations.AnnotationConsolidate])
	require.Empty(t, cmd.Annotations[annotations.AnnotationExclude])

	var found bool

	for _, sub := range cmd.Commands() {
		if sub.Name() == "env" {
			found = true

			break
		}
	}

	require.True(t, found, "project group should host the env subcommand group")
}

// TestProjectCmd_DeprecatedEnvironmentDelegates pins the compatibility contract
// of issue #6057: the former flat names still exist under `project` but are
// hidden, deprecated delegates pointing at the `project env` verbs, and the
// stripped `ls` alias belongs to `project env list` only.
func TestProjectCmd_DeprecatedEnvironmentDelegates(t *testing.T) {
	t.Parallel()

	cmd := projectpkg.NewProjectCmd()

	delegates := map[string]string{
		"add-environment":   `use "ksail project env add" instead`,
		"list-environments": `use "ksail project env list" instead`,
	}

	for _, sub := range cmd.Commands() {
		deprecated, ok := delegates[sub.Name()]
		if !ok {
			continue
		}

		require.True(t, sub.Hidden, "%s delegate should be hidden", sub.Name())
		require.Equal(t, deprecated, sub.Deprecated)
		require.Empty(t, sub.Aliases, "%s delegate should carry no aliases", sub.Name())

		delete(delegates, sub.Name())
	}

	require.Empty(t, delegates, "missing deprecated delegates: %v", delegates)
}

// TestProjectCmd_RejectsArgs pins that the project group rejects positional
// arguments (cobra.NoArgs) instead of silently ignoring them.
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

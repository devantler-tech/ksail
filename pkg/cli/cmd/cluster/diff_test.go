package cluster_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDiffCmd(t *testing.T) {
	t.Parallel()

	cmd := cluster.NewDiffCmd()
	require.NotNil(t, cmd)

	assert.Equal(t, "diff", cmd.Name())
	assert.Equal(t, "Show configuration drift between ksail.yaml and live cluster",
		cmd.Short)
	assert.True(t, cmd.SilenceUsage)

	nameFlag := cmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag)
	assert.Equal(t, "n", nameFlag.Shorthand)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "text", outputFlag.DefValue)

	exitCodeFlag := cmd.Flags().Lookup("exit-code")
	require.NotNil(t, exitCodeFlag)
	assert.Equal(t, "false", exitCodeFlag.DefValue)
}

// TestDiffCmd_InvalidFormatRejectsEarly verifies that an unknown --output
// value is rejected before any cluster interaction takes place.
func TestDiffCmd_InvalidFormatRejectsEarly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
	}{
		{name: "typo jsn", format: "jsn"},
		{name: "empty format", format: ""},
		{name: "xml", format: "xml"},
		{name: "pretty", format: "pretty"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diffCmd := cluster.NewDiffCmd()
			diffCmd.SetOut(io.Discard)
			diffCmd.SetErr(io.Discard)
			diffCmd.SetArgs([]string{"--output", testCase.format})

			err := diffCmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, cluster.ErrUnsupportedOutputFormat,
				"expected ErrUnsupportedOutputFormat for format %q, got: %v",
				testCase.format, err,
			)
		})
	}
}

// TestClusterCmd_RegistersDiffSubcommand verifies that NewClusterCmd wires
// the diff subcommand into the cluster command tree so that toolgen can
// expose it as part of the cluster_read tool.
func TestClusterCmd_RegistersDiffSubcommand(t *testing.T) {
	t.Parallel()

	clusterCmd := cluster.NewClusterCmd()
	require.NotNil(t, clusterCmd)

	diffCmd := findClusterSubcommand(clusterCmd, "diff")
	require.NotNil(t, diffCmd, "expected 'diff' subcommand to be registered")
}

// TestDiffCmd_HasNoWriteAnnotation verifies that the diff command is not
// annotated as a write command, ensuring it appears in cluster_read tools.
func TestDiffCmd_HasNoWriteAnnotation(t *testing.T) {
	t.Parallel()

	cmd := cluster.NewDiffCmd()
	require.NotNil(t, cmd)

	// The diff command is read-only and must NOT have a "write" permission annotation.
	// This ensures toolgen places it under cluster_read, not cluster_write.
	assert.Empty(t, cmd.Annotations["ai.toolgen.permission"],
		"diff command must not have a 'write' permission annotation")
}

// TestDriftExitError verifies that DriftExitError carries exit code 2 and
// that errors.Is(err, ErrDriftDetected) works through the error chain.
func TestDriftExitError(t *testing.T) {
	t.Parallel()

	err := &cluster.DriftExitError{Changes: 3}

	assert.Equal(
		t,
		2,
		err.KSailExitCode(),
		"exit code must be 2 (KSail convention: 0=no drift, 1=error, 2=drift detected)",
	)
	require.ErrorIs(t, err, cluster.ErrDriftDetected,
		"DriftExitError must unwrap to ErrDriftDetected")
	assert.Contains(t, err.Error(), "3")
}

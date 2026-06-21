package open_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/open"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpenCmd(t *testing.T) {
	t.Parallel()

	cmd := open.NewOpenCmd()

	require.NotNil(t, cmd)
	assert.Equal(t, "open", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
}

func TestNewOpenCmd_ExcludeAnnotation(t *testing.T) {
	t.Parallel()

	cmd := open.NewOpenCmd()

	val, ok := cmd.Annotations[annotations.AnnotationExclude]
	assert.True(t, ok, "expected ai.toolgen.exclude annotation to be set")
	assert.Equal(t, "true", val)
}

func TestNewOpenCmd_HasSubcommands(t *testing.T) {
	t.Parallel()

	cmd := open.NewOpenCmd()

	for _, name := range []string{"web", "desktop", "chat", "mcp"} {
		sub := findSubcommand(cmd, name)
		require.NotNil(t, sub, "expected %q subcommand to exist", name)
		assert.NotEmpty(t, sub.Short, "%q should have a short description", name)
		assert.NotEmpty(t, sub.Long, "%q should have a long description", name)
	}
}

func TestOpenCmd_Help(t *testing.T) {
	t.Parallel()

	cmd := open.NewOpenCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	for _, want := range []string{"open", "web", "desktop", "chat", "mcp"} {
		assert.Contains(t, output, want)
	}
}

func TestNewDeprecatedAliases(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"ui":      "ksail open web",
		"desktop": "ksail open desktop",
		"chat":    "ksail open chat",
		"mcp":     "ksail open mcp",
	}

	aliases := open.NewDeprecatedAliases()
	require.Len(t, aliases, len(want))

	for _, cmd := range aliases {
		replacement, ok := want[cmd.Name()]
		require.True(t, ok, "unexpected alias %q", cmd.Name())
		assert.True(t, cmd.Hidden, "alias %q must be hidden", cmd.Name())
		assert.Contains(t, cmd.Deprecated, replacement,
			"alias %q must point at its replacement", cmd.Name())
		assert.NotNil(t, cmd.RunE, "alias %q must still be runnable", cmd.Name())
	}
}

// findSubcommand searches for a subcommand by name.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}

	return nil
}

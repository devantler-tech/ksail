package toolgen_test

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shared literals for the tool-surface safety tests.
const (
	safetyRootUse       = "ksail"
	safetyClusterUse    = "cluster"
	safetySubcmdParam   = "command"
	safetyWritePerm     = "write"
	safetyDeleteSubcmd  = "delete"
	safetyForceFlag     = "force"
	safetyWindowsGOOS   = "windows"
	safetyHelloSubcmd   = "hello"
	safetyEchoBinary    = "echo"
	safetyMissingBinary = "nonexistent_binary_xyz_12345"
	safetyActionParam   = "action"
)

// newConsolidatedRootWithForceFlags builds a root command with a consolidated
// "cluster" parent whose "delete" subcommand carries the confirm-flag
// annotation on --force while "init" does not (init's force means overwrite).
func newConsolidatedRootWithForceFlags(t *testing.T) *cobra.Command {
	t.Helper()

	root := &cobra.Command{Use: safetyRootUse}

	parent := &cobra.Command{
		Use:   safetyClusterUse,
		Short: "Manage cluster lifecycle",
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: safetySubcmdParam,
			annotations.AnnotationPermission:  safetyWritePerm,
		},
	}

	deleteCmd := &cobra.Command{
		Use:   safetyDeleteSubcmd,
		Short: "Destroy a cluster",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	deleteCmd.Flags().
		Bool(safetyForceFlag, false, "Skip confirmation prompt and delete immediately")
	require.NoError(t, deleteCmd.Flags().SetAnnotation(
		safetyForceFlag, annotations.AnnotationConfirmFlag, []string{"true"},
	))

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a cluster project",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	initCmd.Flags().Bool(safetyForceFlag, false, "Overwrite existing files")

	parent.AddCommand(deleteCmd, initCmd)
	root.AddCommand(parent)

	return root
}

// TestGenerateTools_ConfirmFlagAnnotation_Consolidated verifies that the
// ai.toolgen.confirm-flag annotation is carried through SubcommandDef flags
// and resolved per-subcommand by ConfirmFlagsFor.
func TestGenerateTools_ConfirmFlagAnnotation_Consolidated(t *testing.T) {
	t.Parallel()

	root := newConsolidatedRootWithForceFlags(t)
	tools := toolgen.GenerateTools(root, toolgen.ToolOptions{})

	require.Len(t, tools, 1)
	tool := tools[0]
	require.True(t, tool.IsConsolidated)

	require.Contains(t, tool.Subcommands, safetyDeleteSubcmd)
	require.Contains(t, tool.Subcommands, "init")
	assert.True(t, tool.Subcommands[safetyDeleteSubcmd].Flags[safetyForceFlag].ConfirmFlag,
		"annotated delete --force should be marked as confirm flag")
	assert.False(t, tool.Subcommands["init"].Flags[safetyForceFlag].ConfirmFlag,
		"unannotated init --force must not be marked as confirm flag")

	assert.Equal(t, []string{safetyForceFlag},
		tool.ConfirmFlagsFor(map[string]any{safetySubcmdParam: safetyDeleteSubcmd}))
	assert.Empty(t, tool.ConfirmFlagsFor(map[string]any{safetySubcmdParam: "init"}))
	assert.Empty(t, tool.ConfirmFlagsFor(map[string]any{safetySubcmdParam: "missing"}))
	assert.Empty(t, tool.ConfirmFlagsFor(map[string]any{}))
}

// TestGenerateTools_ConfirmFlagAnnotation_NonConsolidated verifies that
// confirm-annotated flags on plain (non-consolidated) tools are listed in
// ToolDefinition.ConfirmFlags and returned by ConfirmFlagsFor.
func TestGenerateTools_ConfirmFlagAnnotation_NonConsolidated(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: safetyRootUse}
	leaf := &cobra.Command{
		Use:   "teardown",
		Short: "Tear down everything",
		Run:   func(_ *cobra.Command, _ []string) {},
	}
	leaf.Flags().Bool(safetyForceFlag, false, "Skip confirmation prompt")
	require.NoError(t, leaf.Flags().SetAnnotation(
		safetyForceFlag, annotations.AnnotationConfirmFlag, []string{"true"},
	))
	leaf.Flags().Bool("verbose", false, "Verbose output")
	root.AddCommand(leaf)

	tools := toolgen.GenerateTools(root, toolgen.ToolOptions{})

	require.Len(t, tools, 1)
	assert.Equal(t, []string{safetyForceFlag}, tools[0].ConfirmFlags)
	assert.Equal(t, []string{safetyForceFlag}, tools[0].ConfirmFlagsFor(map[string]any{}))
}

// TestNormalizeHomePath verifies home-dir prefixes in schema defaults are
// rewritten to the portable "~" form.
func TestNormalizeHomePath(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NotEmpty(t, home)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "home subpath", input: home + "/.kube/config", expected: "~/.kube/config"},
		{
			name:     "outside home unchanged",
			input:    "/etc/kubernetes/admin.conf",
			expected: "/etc/kubernetes/admin.conf",
		},
		{name: "home itself", input: home, expected: "~"},
		{
			name:     "home prefix without separator unchanged",
			input:    home + "x/file",
			expected: home + "x/file",
		},
		{name: "relative path unchanged", input: "k8s/config", expected: "k8s/config"},
		{name: "empty unchanged", input: "", expected: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, toolgen.NormalizeHomePath(testCase.input))
		})
	}
}

// newConsolidatedRootWithSharedFlag builds a consolidated command tree where
// two subcommands share a flag name, with configurable usage strings.
func newConsolidatedRootWithSharedFlag(aUsage, bUsage string) *cobra.Command {
	root := &cobra.Command{Use: safetyRootUse}
	parent := &cobra.Command{
		Use:   "thing",
		Short: "Manage things",
		Annotations: map[string]string{
			annotations.AnnotationConsolidate: safetySubcmdParam,
			annotations.AnnotationPermission:  safetyWritePerm,
		},
	}

	alpha := &cobra.Command{Use: "alpha", Short: "A", Run: func(_ *cobra.Command, _ []string) {}}
	alpha.Flags().String("timeout", "", aUsage)

	beta := &cobra.Command{Use: "beta", Short: "B", Run: func(_ *cobra.Command, _ []string) {}}
	beta.Flags().String("timeout", "", bUsage)

	parent.AddCommand(alpha, beta)
	root.AddCommand(parent)

	return root
}

// flagDescription extracts a flag's description from a generated tool schema.
func flagDescription(t *testing.T, tool toolgen.ToolDefinition, flagName string) string {
	t.Helper()

	properties, propsOK := tool.Parameters["properties"].(map[string]any)
	require.True(t, propsOK, "expected properties map")

	property, propOK := properties[flagName].(map[string]any)
	require.True(t, propOK, "expected property %q", flagName)

	description, descOK := property["description"].(string)
	require.True(t, descOK, "expected description string")

	return description
}

// TestMergedFlagDescriptions_NeutralOnConflict verifies that merged flags with
// differing usage strings get a neutral description instead of first-wins.
func TestMergedFlagDescriptions_NeutralOnConflict(t *testing.T) {
	t.Parallel()

	root := newConsolidatedRootWithSharedFlag(
		"How long to wait for apply", "How long to wait for rollout",
	)
	tools := toolgen.GenerateTools(root, toolgen.ToolOptions{})
	require.Len(t, tools, 1)

	assert.Equal(
		t,
		toolgen.NeutralMergedFlagDescription,
		flagDescription(t, tools[0], "timeout"),
	)
}

// TestMergedFlagDescriptions_PreservedWhenIdentical verifies that identical
// usage strings survive the merge unchanged.
func TestMergedFlagDescriptions_PreservedWhenIdentical(t *testing.T) {
	t.Parallel()

	root := newConsolidatedRootWithSharedFlag("How long to wait", "How long to wait")
	tools := toolgen.GenerateTools(root, toolgen.ToolOptions{})
	require.Len(t, tools, 1)

	assert.Equal(t, "How long to wait", flagDescription(t, tools[0], "timeout"))
}

// TestDefaultExecutablePath verifies the production default resolves the
// running binary so MCP/chat tool execution does not depend on PATH, while
// DefaultOptions stays hermetic for library/test consumers.
func TestDefaultExecutablePath(t *testing.T) {
	t.Parallel()

	expected, err := os.Executable()
	require.NoError(t, err)

	assert.Equal(t, expected, toolgen.DefaultExecutablePath())
	assert.Empty(t, toolgen.DefaultOptions().ExecutablePath,
		"DefaultOptions must not execute the current process by default")
}

// TestResolveCommand verifies executable resolution precedence:
// opts.ExecutablePath first, then the tool's recorded root command name.
func TestResolveCommand(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		CommandParts: []string{safetyRootUse, safetyClusterUse, "info"},
	}

	assert.Equal(t, "/opt/ksail", toolgen.ResolveCommand(
		toolgen.ToolOptions{ExecutablePath: "/opt/ksail"}, tool,
	))
	assert.Equal(t, safetyRootUse, toolgen.ResolveCommand(toolgen.ToolOptions{}, tool))
}

// TestExecuteTool_ExecutablePathOverride verifies the executor runs the
// configured executable instead of the tool's recorded command name.
func TestExecuteTool_ExecutablePathOverride(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == safetyWindowsGOOS {
		t.Skip("echo is a shell builtin on Windows")
	}

	tool := toolgen.ToolDefinition{
		Name:         safetyHelloSubcmd,
		CommandPath:  safetyMissingBinary + " " + safetyHelloSubcmd,
		CommandParts: []string{safetyMissingBinary, safetyHelloSubcmd},
	}

	opts := toolgen.ToolOptions{ExecutablePath: safetyEchoBinary}
	output, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.NoError(t, err)
	assert.Contains(t, output, safetyHelloSubcmd)
}

// TestExecuteTool_WarnsOnInapplicableParams verifies that parameters not
// applicable to the selected subcommand are reported as a warning appended to
// the tool output instead of being dropped silently.
func TestExecuteTool_WarnsOnInapplicableParams(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == safetyWindowsGOOS {
		t.Skip("echo is a shell builtin on Windows")
	}

	tool := toolgen.ToolDefinition{
		Name:            "echo_tools",
		CommandPath:     "echo tools",
		CommandParts:    []string{safetyEchoBinary, "tools"},
		IsConsolidated:  true,
		SubcommandParam: safetyActionParam,
		Subcommands: map[string]*toolgen.SubcommandDef{
			safetyHelloSubcmd: {
				Name:         safetyHelloSubcmd,
				Description:  "Print hello",
				CommandParts: []string{safetyEchoBinary, safetyHelloSubcmd},
				AcceptsArgs:  false,
				Flags:        map[string]*toolgen.FlagDef{},
			},
		},
	}

	params := map[string]any{
		safetyActionParam: safetyHelloSubcmd,
		safetyForceFlag:   true,
		"wait":            "5m",
	}

	output, err := toolgen.ExecuteTool(context.Background(), tool, params, toolgen.ToolOptions{})

	require.NoError(t, err)
	assert.Contains(t, output, safetyHelloSubcmd)
	assert.Contains(
		t,
		output,
		`Warning: ignored parameters not applicable to subcommand "hello": force, wait`,
	)
}

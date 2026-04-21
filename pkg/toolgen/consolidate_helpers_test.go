package toolgen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectAllSubcommands_LeafChildren(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{
		Use:   "cluster",
		Short: "Cluster commands",
	}
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cluster",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a cluster",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	parent.AddCommand(createCmd, deleteCmd)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(parent, &subcommands)

	require.Len(t, subcommands, 2)
	assert.Contains(t, subcommands, "create")
	assert.Contains(t, subcommands, "delete")
	assert.Equal(t, "Create a cluster", subcommands["create"].Description)
}

func TestCollectAllSubcommands_SkipsHiddenCommands(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{
		Use:   "cluster",
		Short: "Cluster commands",
	}
	visibleCmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	hiddenCmd := &cobra.Command{
		Use:    "secret",
		Short:  "Secret command",
		Hidden: true,
		RunE:   func(_ *cobra.Command, _ []string) error { return nil },
	}
	parent.AddCommand(visibleCmd, hiddenCmd)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(parent, &subcommands)

	require.Len(t, subcommands, 1)
	assert.Contains(t, subcommands, "list")
}

func TestCollectAllSubcommands_NestedSubcommands(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{
		Use:   "root",
		Short: "Root",
	}
	groupCmd := &cobra.Command{
		Use:   "group",
		Short: "Group commands",
	}
	leafCmd := &cobra.Command{
		Use:   "action",
		Short: "Do action",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	groupCmd.AddCommand(leafCmd)
	root.AddCommand(groupCmd)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(root, &subcommands)

	// The group is not runnable so only leaf is collected, with prefix
	require.Len(t, subcommands, 1)
	assert.Contains(t, subcommands, "group_action")
	assert.Equal(t, "Do action", subcommands["group_action"].Description)
}

func TestCollectAllSubcommands_RunnableParentWithChildren(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{
		Use:   "root",
		Short: "Root",
	}
	groupCmd := &cobra.Command{
		Use:   "group",
		Short: "Group command",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	// Add a non-help flag to make it "runnable" per isRunnableCommand logic
	groupCmd.Flags().String("config", "", "Config file")

	leafCmd := &cobra.Command{
		Use:   "sub",
		Short: "Sub command",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	groupCmd.AddCommand(leafCmd)
	root.AddCommand(groupCmd)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(root, &subcommands)

	// Both the parent (runnable with non-help flags) and leaf should be collected
	require.Len(t, subcommands, 2)
	assert.Contains(t, subcommands, "group")
	assert.Contains(t, subcommands, "group_sub")
}

func TestCollectAllSubcommands_Empty(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{
		Use:   "parent",
		Short: "Parent with no children",
	}

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(parent, &subcommands)

	assert.Empty(t, subcommands)
}

func TestCollectAllSubcommandsWithPrefix(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{
		Use:   "parent",
		Short: "Parent",
	}
	child := &cobra.Command{
		Use:   "child",
		Short: "Child command",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	parent.AddCommand(child)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommandsWithPrefix(parent, &subcommands, "myprefix")

	require.Len(t, subcommands, 1)
	assert.Contains(t, subcommands, "myprefix_child")
}

func TestCollectAllSubcommandsWithPrefix_EmptyPrefix(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{
		Use:   "parent",
		Short: "Parent",
	}
	child := &cobra.Command{
		Use:   "child",
		Short: "Child command",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	parent.AddCommand(child)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommandsWithPrefix(parent, &subcommands, "")

	require.Len(t, subcommands, 1)
	assert.Contains(t, subcommands, "child")
}

func TestCollectAllSubcommands_AcceptsPositionalArgs(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{
		Use:   "parent",
		Short: "Parent",
	}
	// Command with nil Args (accepts positional args by default)
	acceptsCmd := &cobra.Command{
		Use:   "accepts",
		Short: "Accepts args",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	// Command with NoArgs
	noArgsCmd := &cobra.Command{
		Use:   "noargs",
		Short: "No args",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	parent.AddCommand(acceptsCmd, noArgsCmd)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(parent, &subcommands)

	require.Len(t, subcommands, 2)
	assert.True(t, subcommands["accepts"].AcceptsArgs, "nil Args should accept positional args")
	assert.False(t, subcommands["noargs"].AcceptsArgs, "NoArgs should reject positional args")
}

func TestCollectAllSubcommands_PreservesFlags(t *testing.T) {
	t.Parallel()

	parent := &cobra.Command{
		Use:   "parent",
		Short: "Parent",
	}
	child := &cobra.Command{
		Use:   "child",
		Short: "Child with flags",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	child.Flags().String("name", "", "The name")
	child.Flags().Bool("verbose", false, "Verbose output")
	parent.AddCommand(child)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(parent, &subcommands)

	require.Contains(t, subcommands, "child")
	assert.NotEmpty(t, subcommands["child"].Flags, "flags should be collected")
}

func TestCollectAllSubcommands_DeepNesting(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "root", Short: "Root"}
	level1 := &cobra.Command{Use: "level1", Short: "Level 1"}
	level2 := &cobra.Command{Use: "level2", Short: "Level 2"}
	leaf := &cobra.Command{
		Use:   "leaf",
		Short: "Deeply nested leaf",
		RunE:  func(_ *cobra.Command, _ []string) error { return nil },
	}
	level2.AddCommand(leaf)
	level1.AddCommand(level2)
	root.AddCommand(level1)

	subcommands := make(map[string]*toolgen.SubcommandDef)
	toolgen.CollectAllSubcommands(root, &subcommands)

	require.Len(t, subcommands, 1)
	assert.Contains(t, subcommands, "level1_level2_leaf")
}

package toolgen_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
	"github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

// TestMain isolates the home directory (flag defaults derived from
// os.UserHomeDir() must not leak the developer's real home into snapshots)
// and runs the suite with CI-safe snapshot cleanup.
func TestMain(m *testing.M) {
	os.Exit(homeenv.RunFunc(func() int {
		return snapshottest.Run(m, snaps.CleanOpts{Sort: true})
	}))
}

// toolSurface is the client-visible portion of a generated tool definition:
// everything an MCP client or the Copilot chat assistant sees (name, title,
// description, input schema, permission level, behavioral hints) plus the
// permission split metadata. Runtime-only execution details (CommandParts,
// Subcommands) are intentionally excluded — changing them does not alter the
// wire-visible tool surface.
type toolSurface struct {
	Name               string          `json:"name"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	CommandPath        string          `json:"commandPath"`
	RequiresPermission bool            `json:"requiresPermission"`
	IsConsolidated     bool            `json:"isConsolidated"`
	SubcommandParam    string          `json:"subcommandParam,omitempty"`
	Annotations        toolAnnotations `json:"annotations"`
	Parameters         map[string]any  `json:"parameters"`
}

// toolAnnotations mirrors toolgen.ToolAnnotationHints with JSON tags so the
// snapshot serialization is explicit and stable.
type toolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

// TestToolSurfaceSnapshot is the mechanical gate behind every "tool surface
// unchanged" claim: it generates tools exactly as the MCP server
// (pkg/svc/mcp.NewServer) and the chat assistant (pkg/svc/chat
// GetKSailToolMetadata) do — the real root command + toolgen.DefaultOptions()
// — and snapshots the tool names plus each tool's full JSON schema. Any
// change to the AI-facing tool surface shows up as a snapshot diff;
// intentional changes are re-recorded with UPDATE_SNAPS=true and reviewed.
func TestToolSurfaceSnapshot(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test", "abc123", "2024-01-01")
	tools := toolgen.GenerateTools(root, toolgen.DefaultOptions())

	slices.SortFunc(tools, func(a, b toolgen.ToolDefinition) int {
		return strings.Compare(a.Name, b.Name)
	})

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}

	snaps.MatchSnapshot(t, strings.Join(names, "\n"))

	for _, tool := range tools {
		t.Run(tool.Name, func(t *testing.T) {
			t.Parallel()
			matchToolSurfaceSnapshot(t, tool)
		})
	}
}

// matchToolSurfaceSnapshot serializes the client-visible surface of a single
// tool to stable JSON (encoding/json sorts map keys, so the output is
// deterministic) and matches it against the committed snapshot.
func matchToolSurfaceSnapshot(t *testing.T, tool toolgen.ToolDefinition) {
	t.Helper()

	surface := toolSurface{
		Name:               tool.Name,
		Title:              tool.Title,
		Description:        tool.Description,
		CommandPath:        tool.CommandPath,
		RequiresPermission: tool.RequiresPermission,
		IsConsolidated:     tool.IsConsolidated,
		SubcommandParam:    tool.SubcommandParam,
		Annotations: toolAnnotations{
			ReadOnlyHint:    tool.Annotations.ReadOnlyHint,
			DestructiveHint: tool.Annotations.DestructiveHint,
			IdempotentHint:  tool.Annotations.IdempotentHint,
			OpenWorldHint:   tool.Annotations.OpenWorldHint,
		},
		Parameters: tool.Parameters,
	}

	data, err := json.MarshalIndent(surface, "", "  ")
	require.NoError(t, err)

	snaps.MatchSnapshot(t, normalizeHomeDir(string(data)))
}

// normalizeHomeDir replaces the (test-isolated, per-run random) home
// directory with $HOME so snapshots are stable across runs and machines.
func normalizeHomeDir(content string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return content
	}

	return strings.ReplaceAll(content, home, "$HOME")
}

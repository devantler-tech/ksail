package toolgen

import (
	"log/slog"
	"time"
)

// enumValuer is implemented by flag Value types that provide valid enum values.
// This matches the EnumValuer interface in pkg/apis/cluster/v1alpha1/enums.go.
type enumValuer interface {
	ValidValues() []string
}

// defaulter is implemented by flag Value types that provide a default value.
type defaulter interface {
	Default() any
}

const (
	// defaultCommandTimeout is the default timeout for command execution.
	defaultCommandTimeout = 5 * time.Minute
)

// ToolOptions configures tool generation behavior.
type ToolOptions struct {
	// ExcludeCommands is a list of command paths to exclude (e.g., "ksail chat").
	ExcludeCommands []string
	// IncludeHidden includes hidden commands in tool generation.
	IncludeHidden bool
	// CommandTimeout is the timeout for command execution.
	CommandTimeout time.Duration
	// WorkingDirectory is the directory to run commands in.
	WorkingDirectory string
	// OutputChan receives real-time output chunks from running commands.
	// If nil, output is only available after command completion.
	OutputChan chan<- OutputChunk
	// Logger is used for debug logging during command execution.
	// If nil, no logging is performed.
	Logger *slog.Logger
}

// OutputChunk represents a chunk of output from a running command.
type OutputChunk struct {
	ToolID string // Identifier for the tool that produced this output (for correlation)
	Source string // "stdout" or "stderr"
	Chunk  string // The actual output content
}

// DefaultOptions returns sensible default options for tool generation.
// With permission-based consolidation, all workload and cluster commands are grouped
// into 6 tools: cluster_read, cluster_write, workload_read, workload_write, cipher_read, and cipher_write.
func DefaultOptions() ToolOptions {
	return ToolOptions{
		ExcludeCommands: []string{
			// Meta commands - not useful as tools
			"ksail chat",              // Chat interface, not a tool
			"ksail mcp",               // MCP server itself, not a tool
			"ksail completion",        // Shell completion generator
			"ksail generate-fig-spec", // Fig/Kiro CLI completion spec generator
			"ksail help",              // Help command, not a tool
			"ksail",                   // Root command, not a tool
		},
		IncludeHidden:  false,
		CommandTimeout: defaultCommandTimeout,
	}
}

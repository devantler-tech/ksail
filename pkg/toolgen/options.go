package toolgen

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// enumValuer is implemented by flag Value types that provide valid enum values.
// This matches the EnumValuer interface in pkg/apis/cluster/v1alpha1/enum.go.
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
	// SessionLog is an optional shared reference to a session log function.
	// Set after session creation to enable SDK-native logging from tool handlers.
	// Tool handlers check if the function is set before calling.
	SessionLog *SessionLogRef
}

// SessionLogRef holds a session log function that can be set after session creation.
// It is safe for concurrent read/write access.
type SessionLogRef struct {
	mu sync.RWMutex
	fn func(ctx context.Context, message, level string)
}

// NewSessionLogRef creates a new empty SessionLogRef.
func NewSessionLogRef() *SessionLogRef {
	return &SessionLogRef{}
}

// Set configures the log function. Call after session creation.
func (r *SessionLogRef) Set(fn func(ctx context.Context, message, level string)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.fn = fn
}

// Log sends a log message to the session. No-op if the function is not set.
func (r *SessionLogRef) Log(ctx context.Context, message, level string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.fn != nil {
		r.fn(ctx, message, level)
	}
}

// OutputChunk represents a chunk of output from a running command.
type OutputChunk struct {
	ToolID string // Identifier for the tool that produced this output (for correlation)
	Source string // "stdout" or "stderr"
	Chunk  string // The actual output content
}

// DefaultOptions returns sensible default options for tool generation.
// With permission-based consolidation, commands are grouped by their permission
// annotations (read vs write) and scope (cluster vs workload, plus cipher) so that
// many individual commands collapse into 5 consolidated tools:
// cluster_read, cluster_write, workload_read, workload_write, and cipher_write.
func DefaultOptions() ToolOptions {
	return ToolOptions{
		ExcludeCommands: []string{
			// Meta commands - not useful as tools
			"ksail chat",       // Chat interface, not a tool
			"ksail mcp",        // MCP server itself, not a tool
			"ksail completion", // Shell completion generator
			"ksail help",       // Help command, not a tool
			"ksail",            // Root command, not a tool
		},
		IncludeHidden:  false,
		CommandTimeout: defaultCommandTimeout,
	}
}

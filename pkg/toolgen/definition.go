package toolgen

// ToolDefinition is an SDK-agnostic representation of a tool generated from a Cobra command.
// It contains all the metadata needed to create SDK-specific tools (Copilot, MCP, etc.).
type ToolDefinition struct {
	// Name is the tool identifier (e.g., "cluster_create", "workload_read").
	Name string

	// Title is a human-readable display name for the tool (e.g., "Cluster Create", "Workload Read").
	// Used by MCP clients for UI display. Precedence: Title > Name.
	Title string

	// Description provides context for the AI about what the tool does.
	Description string

	// Parameters is a map of parameter names to their JSON schema properties.
	// Format: map[paramName]map[string]any where inner map has "type", "description", etc.
	Parameters map[string]any

	// CommandPath is the full command path (e.g., "ksail cluster create").
	CommandPath string

	// CommandParts contains the command split into parts (e.g., ["ksail", "cluster", "create"]).
	CommandParts []string

	// RequiresPermission indicates if this tool performs edit operations.
	RequiresPermission bool

	// Annotations holds MCP behavioral hints for the tool.
	// These help clients decide on auto-approval, warnings, and retry behavior.
	Annotations ToolAnnotationHints

	// IsConsolidated indicates if this tool represents multiple subcommands.
	IsConsolidated bool

	// SubcommandParam is the name of the parameter that selects the subcommand.
	// Only set when IsConsolidated is true (e.g., "resource_type", "action", "operation").
	SubcommandParam string

	// Subcommands maps subcommand names to their metadata.
	// Only populated when IsConsolidated is true.
	Subcommands map[string]*SubcommandDef
}

// ToolAnnotationHints contains SDK-agnostic behavioral hints for a tool.
// These map to MCP ToolAnnotations and inform client behavior.
// KSail always sets all fields explicitly so there is no ambiguity.
type ToolAnnotationHints struct {
	// ReadOnlyHint indicates the tool does not modify its environment.
	ReadOnlyHint bool

	// DestructiveHint indicates the tool may perform destructive updates.
	DestructiveHint bool

	// IdempotentHint indicates repeated calls with the same args have no additional effect.
	// Set true for both read-only tools (reads are inherently idempotent) and
	// write tools whose operations are idempotent (e.g., declarative apply).
	IdempotentHint bool

	// OpenWorldHint indicates the tool may interact with external entities.
	// Always true for KSail tools because they can target remote clusters,
	// external cloud providers (Hetzner, AWS EKS, Omni), and OCI registries.
	OpenWorldHint bool
}

// Parameter represents a single tool parameter extracted from a Cobra flag.
type Parameter struct {
	Name        string
	Type        string // JSON schema type: "string", "integer", "boolean", "array", "object"
	Description string
	Required    bool
	Default     any
	Enum        []string   // Valid values for enum types
	Items       *Parameter // For array types
}

// SubcommandDef contains metadata about a subcommand in a consolidated tool.
type SubcommandDef struct {
	// Name is the subcommand name (e.g., "deployment", "restart", "encrypt").
	Name string

	// Description describes what this subcommand does.
	Description string

	// CommandParts are the full command parts for execution.
	CommandParts []string

	// Flags contains metadata about flags specific to or modified by this subcommand.
	Flags map[string]*FlagDef

	// AcceptsArgs indicates whether this subcommand accepts positional arguments.
	AcceptsArgs bool
}

// FlagDef contains metadata about a flag in a consolidated tool.
type FlagDef struct {
	// Name is the flag name.
	Name string

	// Type is the JSON schema type.
	Type string

	// ItemsType is the JSON schema type for array items (e.g., "string", "integer").
	// Only populated when Type is "array".
	ItemsType string

	// Description describes the flag.
	Description string

	// Required indicates if this flag is required.
	Required bool

	// Default is the default value.
	Default any

	// AppliesToSubcommands lists which subcommands this flag applies to.
	// Empty means it applies to all subcommands.
	AppliesToSubcommands []string
}

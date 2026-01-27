// Package annotations provides constants for annotating Cobra commands to control
// AI tool generation, permission requirements, and command consolidation.
//
// These annotations are used by the toolgen package to generate AI-accessible tools
// and by the chat interface to enforce permission checks for edit operations.
//
// # Available Annotations
//
// AnnotationExclude: Exclude a command and all its subcommands from AI tool generation.
// Set to "true" to exclude. Useful for meta commands like chat, mcp, completion.
//
//	cmd.Annotations = map[string]string{
//	    annotations.AnnotationExclude: "true",
//	}
//
// AnnotationDescription: Override the default description for AI tool generation.
// If not set, the command's Short description is used.
//
//	cmd.Annotations = map[string]string{
//	    annotations.AnnotationDescription: "Custom description for AI",
//	}
//
// AnnotationPermission: Mark commands that modify state and require user confirmation.
// Set to "write" for any operation that creates, updates, or deletes resources.
//
//	cmd.Annotations = map[string]string{
//	    annotations.AnnotationPermission: "write",
//	}
//
// AnnotationConsolidate: Consolidate subcommands into a single AI tool with an enum parameter.
// The value specifies the parameter name for subcommand selection.
//
//	cmd.Annotations = map[string]string{
//	    annotations.AnnotationConsolidate: "resource_type", // or "action", "operation"
//	}
package annotations

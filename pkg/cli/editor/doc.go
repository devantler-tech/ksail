// Package editor provides editor configuration resolution for CLI commands.
//
// This package handles resolving the appropriate text editor for various
// CLI operations (cipher edit, workload edit, cluster connect) with proper
// precedence: flags > config > environment variables > fallback editors.
package editor

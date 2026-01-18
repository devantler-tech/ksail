package configmanager

import (
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
)

// LoadOptions configures how configuration is loaded.
type LoadOptions struct {
	// Timer enables timing output in notifications when provided.
	Timer timer.Timer
	// Silent suppresses all loading notifications when true.
	Silent bool
	// IgnoreConfigFile skips reading on-disk config files when true (flags/defaults only).
	IgnoreConfigFile bool
	// SkipValidation skips config validation when true.
	// Useful for commands that only need partial config (e.g., context/kubeconfig).
	SkipValidation bool
}

// ConfigManager provides configuration management functionality.
//
//go:generate mockery
type ConfigManager[T any] interface {
	// Load loads the configuration with the specified options.
	// Returns the loaded config, either freshly loaded or previously cached.
	Load(opts LoadOptions) (*T, error)
}

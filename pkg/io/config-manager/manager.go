package configmanager

import "github.com/devantler-tech/ksail-go/pkg/ui/timer"

// ConfigManager provides configuration management functionality.
//
//go:generate mockery
type ConfigManager[T any] interface {
	// LoadConfig loads the configuration from files and environment variables.
	// Returns the loaded config, either freshly loaded or previously cached.
	// If timer is provided, timing information will be included in the success notification.
	LoadConfig(tmr timer.Timer) (*T, error)
}

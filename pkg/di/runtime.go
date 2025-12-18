package di

import (
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

// Core types.

// Module registers dependencies with the DI container.
// Each module is a function that configures one or more dependencies.
type Module func(do.Injector) error

// Injector is an alias for the underlying DI container implementation.
// This abstraction allows the codebase to remain independent of the specific DI library used.
type Injector = do.Injector

// Runtime container.

// Runtime owns shared dependency registration for Cobra commands.
// It maintains a list of base modules that are applied to every invocation.
type Runtime struct {
	baseModules []Module
}

// New constructs a Runtime with the provided base modules. Modules are applied
// in the order supplied when invoking commands.
func New(modules ...Module) *Runtime {
	return &Runtime{
		baseModules: append([]Module{}, modules...),
	}
}

// Invoke builds a fresh injector, applies base and extra modules, and executes
// the provided handler function. The injector is automatically shut down after
// the handler completes.
func (r *Runtime) Invoke(
	handler func(Injector) error,
	extraModules ...Module,
) error {
	injector := do.New()

	defer func() {
		_ = injector.Shutdown()
	}()

	modules := append([]Module{}, r.baseModules...)
	modules = append(modules, extraModules...)

	for _, module := range modules {
		if module == nil {
			continue
		}

		err := module(injector)
		if err != nil {
			return err
		}
	}

	return handler(injector)
}

// Cobra integration.

// RunEWithRuntime returns a cobra RunE function that resolves dependencies using
// the provided runtime container. The handler is executed with the active command
// and resolved injector when the command runs.
func RunEWithRuntime(
	runtimeContainer *Runtime,
	handler func(cmd *cobra.Command, injector Injector) error,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		return runtimeContainer.Invoke(func(injector Injector) error {
			return handler(cmd, injector)
		})
	}
}

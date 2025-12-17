// Package di exposes shared dependency injection helpers for KSail commands.
//
// This package provides a runtime container for registering and resolving
// dependencies used by Cobra commands, enabling testable and modular
// command implementations through dependency injection patterns.
//
// # Key Components
//
// Runtime: A container that manages dependency registration and lifecycle.
// Module: A function that registers one or more dependencies with the injector.
// Injector: The underlying DI container (aliased from samber/do).
//
// # Usage
//
// Create a runtime with default dependencies:
//
//	runtime := di.NewRuntime()
//
// Use the runtime in a Cobra command:
//
//	cmd.RunE = di.RunEWithRuntime(runtime, func(cmd *cobra.Command, injector di.Injector) error {
//		timer, err := di.ResolveTimer(injector)
//		// ... use dependencies
//	})
//
// Or use a decorator to automatically resolve dependencies:
//
//	cmd.RunE = di.RunEWithRuntime(runtime, di.WithTimer(
//		func(cmd *cobra.Command, injector di.Injector, timer timer.Timer) error {
//			// timer is already resolved
//		}
//	))
//
// # Testing
//
// In tests, create a runtime with mock dependencies:
//
//	mockTimer := timer.NewMockTimer(t)
//	runtime := di.New(func(i di.Injector) error {
//		do.Provide(i, func(do.Injector) (timer.Timer, error) {
//			return mockTimer, nil
//		})
//		return nil
//	})
package di

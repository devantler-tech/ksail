package di

import (
	"fmt"

	clusterprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

// Dependency resolvers.

// ResolveTimer retrieves the timer dependency from the injector with consistent error handling.
//
//nolint:ireturn,nolintlint // DI container exposes the timer interface.
func ResolveTimer(injector Injector) (timer.Timer, error) {
	tmr, err := do.Invoke[timer.Timer](injector)
	if err != nil {
		return nil, fmt.Errorf("resolve timer dependency: %w", err)
	}

	return tmr, nil
}

// ResolveClusterProvisionerFactory retrieves the cluster provisioner factory dependency
// from the injector with consistent error handling.
//
//nolint:ireturn,nolintlint // DI container exposes the factory interface.
func ResolveClusterProvisionerFactory(
	injector Injector,
) (clusterprovisioner.Factory, error) {
	factory, err := do.Invoke[clusterprovisioner.Factory](injector)
	if err != nil {
		return nil, fmt.Errorf("resolve provisioner factory dependency: %w", err)
	}

	return factory, nil
}

// Handler decorators.

// WithTimer decorates a handler to automatically resolve the timer dependency.
// This higher-order function simplifies command handlers that need timer access.
func WithTimer(
	handler func(cmd *cobra.Command, injector Injector, tmr timer.Timer) error,
) func(cmd *cobra.Command, injector Injector) error {
	return func(cmd *cobra.Command, injector Injector) error {
		tmr, err := ResolveTimer(injector)
		if err != nil {
			return err
		}

		return handler(cmd, injector, tmr)
	}
}

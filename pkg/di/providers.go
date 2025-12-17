package di

import (
	clusterprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	"github.com/samber/do/v2"
)

// Dependency providers.

// NewRuntime constructs the shared runtime container used by root command and tests.
// It registers default implementations for timer and cluster provisioner factory.
func NewRuntime() *Runtime {
	return New(
		provideTimer,
		provideClusterProvisionerFactory,
	)
}

// provideTimer registers the timer dependency with the injector.
func provideTimer(i Injector) error {
	do.Provide(i, func(Injector) (timer.Timer, error) {
		return timer.New(), nil
	})

	return nil
}

// provideClusterProvisionerFactory registers the cluster provisioner factory dependency.
func provideClusterProvisionerFactory(i Injector) error {
	do.Provide(i, func(Injector) (clusterprovisioner.Factory, error) {
		return clusterprovisioner.DefaultFactory{}, nil
	})

	return nil
}

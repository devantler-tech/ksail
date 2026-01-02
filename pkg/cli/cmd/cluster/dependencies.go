package cluster

import (
	"context"
	"sync"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// Package-level dependencies for cluster commands.
// These variables support dependency injection for testing while providing production defaults.
// Use the Set*ForTests functions to override these values in tests.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverrideMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverride *setup.InstallerFactories
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvokerMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryOverride clusterprovisioner.Factory
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvoker = helpers.WithDockerClient
)

// getInstallerFactories returns the installer factories to use, allowing test override.
func getInstallerFactories() *setup.InstallerFactories {
	installerFactoriesOverrideMu.RLock()
	defer installerFactoriesOverrideMu.RUnlock()

	if installerFactoriesOverride != nil {
		return installerFactoriesOverride
	}

	return setup.DefaultInstallerFactories()
}

// overrideInstallerFactory is a helper that applies a factory override and returns a restore function.
func overrideInstallerFactory(apply func(*setup.InstallerFactories)) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := setup.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	apply(override)
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()

		installerFactoriesOverride = previous

		installerFactoriesOverrideMu.Unlock()
	}
}

// SetCertManagerInstallerFactoryForTests overrides the cert-manager installer factory.
func SetCertManagerInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.CertManager = factory
	})
}

// SetCSIInstallerFactoryForTests overrides the CSI installer factory.
func SetCSIInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.CSI = factory
	})
}

// SetArgoCDInstallerFactoryForTests overrides the Argo CD installer factory.
func SetArgoCDInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.ArgoCD = factory
	})
}

// SetEnsureArgoCDResourcesForTests overrides the Argo CD resource ensure function.
func SetEnsureArgoCDResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureArgoCDResources = fn
	})
}

// SetDockerClientInvokerForTests overrides the Docker client invoker for testing.
func SetDockerClientInvokerForTests(
	invoker func(*cobra.Command, func(client.APIClient) error) error,
) func() {
	dockerClientInvokerMu.Lock()

	previous := dockerClientInvoker
	dockerClientInvoker = invoker

	dockerClientInvokerMu.Unlock()

	return func() {
		dockerClientInvokerMu.Lock()

		dockerClientInvoker = previous

		dockerClientInvokerMu.Unlock()
	}
}

// SetClusterProvisionerFactoryForTests overrides the cluster provisioner factory for testing.
func SetClusterProvisionerFactoryForTests(factory clusterprovisioner.Factory) func() {
	clusterProvisionerFactoryMu.Lock()

	previous := clusterProvisionerFactoryOverride
	clusterProvisionerFactoryOverride = factory

	clusterProvisionerFactoryMu.Unlock()

	return func() {
		clusterProvisionerFactoryMu.Lock()

		clusterProvisionerFactoryOverride = previous

		clusterProvisionerFactoryMu.Unlock()
	}
}

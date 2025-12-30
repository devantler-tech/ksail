package cluster

import (
	"context"
	"sync"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// Test injection for installer factories, docker client invoker, and cluster provisioner factory.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverrideMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverride *create.InstallerFactories
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
func getInstallerFactories() *create.InstallerFactories {
	installerFactoriesOverrideMu.RLock()
	defer installerFactoriesOverrideMu.RUnlock()

	if installerFactoriesOverride != nil {
		return installerFactoriesOverride
	}

	return create.DefaultInstallerFactories()
}

// overrideInstallerFactory is a helper that applies a factory override and returns a restore function.
func overrideInstallerFactory(apply func(*create.InstallerFactories)) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := create.DefaultInstallerFactories()

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
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
		f.CertManager = factory
	})
}

// SetCSIInstallerFactoryForTests overrides the CSI installer factory.
func SetCSIInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
		f.CSI = factory
	})
}

// SetArgoCDInstallerFactoryForTests overrides the Argo CD installer factory.
func SetArgoCDInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
		f.ArgoCD = factory
	})
}

// SetEnsureArgoCDResourcesForTests overrides the Argo CD resource ensure function.
func SetEnsureArgoCDResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster) error,
) func() {
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
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

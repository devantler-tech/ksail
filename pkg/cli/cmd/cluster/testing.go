package cluster

import (
	"context"
	"io"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

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
	fn func(context.Context, string, *v1alpha1.Cluster, string) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureArgoCDResources = fn
	})
}

// SetFluxInstallerFactoryForTests overrides the Flux installer factory.
func SetFluxInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		// Wrap the simplified test factory to match the Flux factory signature
		f.Flux = func(_ helm.Interface, _ time.Duration) installer.Installer {
			inst, _ := factory(nil) // clusterCfg not used in test factory

			return inst
		}
	})
}

// SetEnsureFluxResourcesForTests overrides the Flux resource ensure function.
func SetEnsureFluxResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster, string, bool) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureFluxResources = fn
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

// SetLocalRegistryServiceFactoryForTests overrides the local registry service factory for testing.
func SetLocalRegistryServiceFactoryForTests(factory localregistry.ServiceFactoryFunc) func() {
	localRegistryServiceFactoryMu.Lock()

	previous := localRegistryServiceFactory
	localRegistryServiceFactory = factory

	localRegistryServiceFactoryMu.Unlock()

	return func() {
		localRegistryServiceFactoryMu.Lock()

		localRegistryServiceFactory = previous

		localRegistryServiceFactoryMu.Unlock()
	}
}

// SetSetupFluxInstanceForTests overrides the FluxInstance setup function.
func SetSetupFluxInstanceForTests(
	fn func(context.Context, string, *v1alpha1.Cluster, string) error,
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.SetupFluxInstance = fn
	})
}

// SetWaitForFluxReadyForTests overrides the Flux readiness wait function.
func SetWaitForFluxReadyForTests(fn func(context.Context, string) error) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.WaitForFluxReady = fn
	})
}

// SetEnsureOCIArtifactForTests overrides the OCI artifact ensure function.
func SetEnsureOCIArtifactForTests(
	fn func(context.Context, *cobra.Command, *v1alpha1.Cluster, string, io.Writer) (bool, error),
) func() {
	return overrideInstallerFactory(func(f *setup.InstallerFactories) {
		f.EnsureOCIArtifact = fn
	})
}

package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/api/types/container"
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
	//nolint:gochecknoglobals // dependency injection for tests
	localRegistryServiceFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	localRegistryServiceFactory localregistry.ServiceFactoryFunc
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

// getLocalRegistryDeps returns the local registry dependencies, respecting any test overrides.
func getLocalRegistryDeps() localregistry.Dependencies {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	opts := []localregistry.Option{
		localregistry.WithDockerInvoker(invoker),
	}

	localRegistryServiceFactoryMu.RLock()

	factory := localRegistryServiceFactory

	localRegistryServiceFactoryMu.RUnlock()

	if factory != nil {
		opts = append(opts, localregistry.WithServiceFactory(factory))
	}

	return localregistry.NewDependencies(opts...)
}

// getCleanupDeps returns the cleanup dependencies for mirror registry operations.
func getCleanupDeps() mirrorregistry.CleanupDependencies {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	return mirrorregistry.CleanupDependencies{
		DockerInvoker:     invoker,
		LocalRegistryDeps: getLocalRegistryDeps(),
	}
}

// withDockerClient executes an operation with the Docker client, handling locking and invoker retrieval.
// This is the canonical way to access Docker in this package, ensuring thread-safe access to the invoker.
func withDockerClient(cmd *cobra.Command, operation func(client.APIClient) error) error {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	return invoker(cmd, operation)
}

// forEachContainerName lists all Docker containers and calls the provided function for each container name.
// The function receives the normalized container name (without leading slash).
// Container processing stops early if the callback returns true (indicating done).
func forEachContainerName(
	cmd *cobra.Command,
	callback func(containerName string) (done bool),
) error {
	return forEachContainer(
		cmd,
		func(_ client.APIClient, _ container.Summary, name string) error {
			if callback(name) {
				return errStopIteration
			}

			return nil
		},
	)
}

// errStopIteration is a sentinel error used to stop container iteration early.
var errStopIteration = errors.New("stop iteration")

// forEachContainer lists all Docker containers and calls the callback for each container name.
// The callback receives the docker client, container info, and normalized container name.
// Return an error to stop iteration (use errStopIteration for normal early exit).
func forEachContainer(
	cmd *cobra.Command,
	callback func(dockerClient client.APIClient, ctr container.Summary, name string) error,
) error {
	return withDockerClient(cmd, func(dockerClient client.APIClient) error {
		containers, err := dockerClient.ContainerList(cmd.Context(), container.ListOptions{
			All: true,
		})
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		for _, ctr := range containers {
			for _, name := range ctr.Names {
				containerName := strings.TrimPrefix(name, "/")

				err := callback(dockerClient, ctr, containerName)
				if err != nil {
					if errors.Is(err, errStopIteration) {
						return nil // Normal early exit
					}

					return err
				}
			}
		}

		return nil
	})
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

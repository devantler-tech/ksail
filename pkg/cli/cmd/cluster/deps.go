package cluster

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/devantler-tech/ksail/v5/pkg/cli/dockerutil"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// Package-level dependencies for cluster commands.
// These variables support dependency injection for testing while providing production defaults.
// Use the Set*ForTests functions in testing.go to override these values in tests.
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
	dockerClientInvoker = dockerutil.WithDockerClient
	//nolint:gochecknoglobals // dependency injection for tests
	localRegistryServiceFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	localRegistryServiceFactory localregistry.ServiceFactoryFunc
)

// errStopIteration is a sentinel error used to stop container iteration early.
var errStopIteration = errors.New("stop iteration")

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

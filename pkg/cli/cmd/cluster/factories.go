package cluster

import (
	"sync"

	"github.com/devantler-tech/ksail/v7/pkg/cli/dockerutil"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
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

// getInstallerFactories returns the installer factories to use, allowing test override.
func getInstallerFactories() *setup.InstallerFactories {
	installerFactoriesOverrideMu.RLock()
	defer installerFactoriesOverrideMu.RUnlock()

	if installerFactoriesOverride != nil {
		return installerFactoriesOverride
	}

	return setup.DefaultInstallerFactories()
}

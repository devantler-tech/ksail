package docker_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// Package-level sinks prevent the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variables are required to prevent compiler optimization.
var (
	benchRegistryManagerSink *docker.RegistryManager
	benchRegistryConfigSink  *dockertypes.Config
	benchHostConfigSink      *dockertypes.HostConfig
	benchNetworkConfigSink   *network.NetworkingConfig
	benchVolumeName          string
)

// BenchmarkNewRegistryManager benchmarks the creation of a RegistryManager.
// This happens once per cluster provisioning operation.
func BenchmarkNewRegistryManager(b *testing.B) {
	mockClient := new(docker.MockAPIClient)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchRegistryManagerSink, errBench = docker.NewRegistryManager(mockClient)
		if errBench != nil {
			b.Fatalf("NewRegistryManager failed: %v", errBench)
		}
	}
}

// BenchmarkNewRegistryManagerNilClient benchmarks error handling for nil client.
// This validates input validation overhead.
func BenchmarkNewRegistryManagerNilClient(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, errBench = docker.NewRegistryManager(nil)
		// Expected error - don't fail
	}
}

// minimalRegistryConfig returns a minimal valid registry configuration for benchmarking.
func minimalRegistryConfig() docker.RegistryConfig {
	return docker.RegistryConfig{
		Name:        "test-registry",
		Port:        5000,
		UpstreamURL: "https://registry-1.docker.io",
	}
}

// productionRegistryConfig returns a production-like registry configuration.
func productionRegistryConfig() docker.RegistryConfig {
	return docker.RegistryConfig{
		Name:        "prod-registry-mirror",
		Port:        5001,
		UpstreamURL: "https://ghcr.io",
		Username:    "user",
		Password:    "password",
	}
}

// setupBenchmarkRegistryManager creates a registry manager with a mock client for benchmarking.
func setupBenchmarkRegistryManager() *docker.RegistryManager {
	mockClient := new(docker.MockAPIClient)

	manager, err := docker.NewRegistryManager(mockClient)
	if err != nil {
		panic("failed to create benchmark registry manager: " + err.Error())
	}

	return manager
}

// The following benchmarks test internal configuration builders.
// These are exported for testing purposes and measure the overhead
// of building Docker container configurations.

// BenchmarkBuildContainerConfig benchmarks container config generation.
// This happens during registry creation (inlined in real usage).
func BenchmarkBuildContainerConfig_Minimal(b *testing.B) {
	registryManager := setupBenchmarkRegistryManager()
	config := minimalRegistryConfig()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		containerConfig, err := registryManager.ExportBuildContainerConfig(config)
		if err != nil {
			b.Fatalf("ExportBuildContainerConfig failed: %v", err)
		}

		benchRegistryConfigSink = containerConfig
	}
}

// BenchmarkBuildContainerConfig_Production benchmarks container config with authentication.
func BenchmarkBuildContainerConfig_Production(b *testing.B) {
	registryManager := setupBenchmarkRegistryManager()
	config := productionRegistryConfig()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		containerConfig, err := registryManager.ExportBuildContainerConfig(config)
		if err != nil {
			b.Fatalf("ExportBuildContainerConfig failed: %v", err)
		}

		benchRegistryConfigSink = containerConfig
	}
}

// BenchmarkBuildHostConfig benchmarks host config generation.
func BenchmarkBuildHostConfig_Minimal(b *testing.B) {
	registryManager := setupBenchmarkRegistryManager()
	config := minimalRegistryConfig()
	volumeName := "test-volume"

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchHostConfigSink = registryManager.ExportBuildHostConfig(config, volumeName)
	}
}

// BenchmarkBuildNetworkConfig benchmarks network config generation.
func BenchmarkBuildNetworkConfig_Minimal(b *testing.B) {
	registryManager := setupBenchmarkRegistryManager()
	config := minimalRegistryConfig()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchNetworkConfigSink = registryManager.ExportBuildNetworkConfig(config)
	}
}

// BenchmarkResolveVolumeName benchmarks volume name resolution.
func BenchmarkResolveVolumeName_Minimal(b *testing.B) {
	registryManager := setupBenchmarkRegistryManager()
	config := minimalRegistryConfig()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchVolumeName = registryManager.ExportResolveVolumeName(config)
	}
}

// BenchmarkBuildProxyCredentialsEnv benchmarks credential environment variable building.
func BenchmarkBuildProxyCredentialsEnv_WithCredentials(b *testing.B) {
	registryManager := setupBenchmarkRegistryManager()
	config := productionRegistryConfig()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env, err := registryManager.ExportBuildProxyCredentialsEnv(config.Username, config.Password)
		if err != nil {
			b.Fatalf("ExportBuildProxyCredentialsEnv failed: %v", err)
		}
		_ = env // Use the result
	}
}

// BenchmarkBuildProxyCredentialsEnv_NoCredentials benchmarks the case without credentials.
func BenchmarkBuildProxyCredentialsEnv_NoCredentials(b *testing.B) {
	registryManager := setupBenchmarkRegistryManager()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		env, err := registryManager.ExportBuildProxyCredentialsEnv("", "")
		if err != nil {
			b.Fatalf("ExportBuildProxyCredentialsEnv failed: %v", err)
		}
		_ = env // Use the result
	}
}

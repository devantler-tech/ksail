package docker_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	dockerclient "github.com/docker/docker/client"
)

// Package-level sinks prevent the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variables are required to prevent compiler optimization.
var (
	benchAPIClientSink     dockerclient.APIClient
	benchConcreteClientSink *dockerclient.Client
	benchError             error
)

// BenchmarkGetDockerClient benchmarks the creation of a Docker client from environment.
// This is a critical operation that happens during cluster provisioning.
func BenchmarkGetDockerClient(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchAPIClientSink, benchError = docker.GetDockerClient()
		if benchError != nil {
			b.Fatalf("GetDockerClient failed: %v", benchError)
		}
	}
}

// BenchmarkGetConcreteDockerClient benchmarks creation and type assertion to concrete client.
// This is used when callers need the concrete *client.Client type for advanced operations.
func BenchmarkGetConcreteDockerClient(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchConcreteClientSink, benchError = docker.GetConcreteDockerClient()
		if benchError != nil {
			b.Fatalf("GetConcreteDockerClient failed: %v", benchError)
		}
	}
}

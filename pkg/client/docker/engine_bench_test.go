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
	benchAPIClientSink      dockerclient.APIClient
	benchConcreteClientSink *dockerclient.Client
	errBench                error
)

// BenchmarkGetDockerClient benchmarks the creation of a Docker client from environment.
// This is a critical operation that happens during cluster provisioning.
func BenchmarkGetDockerClient(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchAPIClientSink, errBench = docker.GetDockerClient()
		if errBench != nil {
			b.Fatalf("GetDockerClient failed: %v", errBench)
		}
	}
}

// BenchmarkGetConcreteDockerClient benchmarks creation and type assertion to concrete client.
// This is used when callers need the concrete *client.Client type for advanced operations.
func BenchmarkGetConcreteDockerClient(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchConcreteClientSink, errBench = docker.GetConcreteDockerClient()
		if errBench != nil {
			b.Fatalf("GetConcreteDockerClient failed: %v", errBench)
		}
	}
}

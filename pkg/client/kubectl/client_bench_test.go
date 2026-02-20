package kubectl

import (
	"bytes"
	"testing"

	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// BenchmarkCreateClient benchmarks the creation of a new kubectl client.
func BenchmarkCreateClient(b *testing.B) {
	streams := genericiooptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewClient(streams)
	}
}

// BenchmarkCreateApplyCommand benchmarks the creation of an apply command.
func BenchmarkCreateApplyCommand(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.CreateApplyCommand("/tmp/kubeconfig")
	}
}

// BenchmarkCreateGetCommand benchmarks the creation of a get command.
func BenchmarkCreateGetCommand(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.CreateGetCommand("/tmp/kubeconfig")
	}
}

// BenchmarkCreateDeleteCommand benchmarks the creation of a delete command.
func BenchmarkCreateDeleteCommand(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.CreateDeleteCommand("/tmp/kubeconfig")
	}
}

// BenchmarkCreateDescribeCommand benchmarks the creation of a describe command.
func BenchmarkCreateDescribeCommand(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.CreateDescribeCommand("/tmp/kubeconfig")
	}
}

// BenchmarkCreateLogsCommand benchmarks the creation of a logs command.
func BenchmarkCreateLogsCommand(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.CreateLogsCommand("/tmp/kubeconfig")
	}
}

// BenchmarkCreateWaitCommand benchmarks the creation of a wait command.
func BenchmarkCreateWaitCommand(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.CreateWaitCommand("/tmp/kubeconfig")
	}
}

// BenchmarkCreateNamespaceCmd benchmarks the creation of a namespace generator command.
func BenchmarkCreateNamespaceCmd(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.CreateNamespaceCmd()
	}
}

// BenchmarkCreateDeploymentCmd benchmarks the creation of a deployment generator command.
func BenchmarkCreateDeploymentCmd(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.CreateDeploymentCmd()
	}
}

// BenchmarkCreateServiceCmd benchmarks the creation of a service generator command.
func BenchmarkCreateServiceCmd(b *testing.B) {
	client := setupBenchClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.CreateServiceCmd()
	}
}

// Helper function to set up a client for benchmarking.
func setupBenchClient() *Client {
	return NewClient(genericiooptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	})
}

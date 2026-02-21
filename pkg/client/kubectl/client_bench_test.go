package kubectl

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// Package-level sinks prevent the compiler from optimizing away benchmark calls.
var (
	benchClientSink  *Client
	benchCommandSink *cobra.Command
)

// minimalKubeconfig is a valid empty kubeconfig used by command-creation benchmarks.
const minimalKubeconfig = `apiVersion: v1
clusters: []
contexts: []
current-context: ""
kind: Config
preferences: {}
users: []
`

// writeBenchKubeconfig writes a minimal kubeconfig into b.TempDir and returns its path.
func writeBenchKubeconfig(b *testing.B) string {
	b.Helper()

	path := filepath.Join(b.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(minimalKubeconfig), 0o600); err != nil {
		b.Fatalf("failed to write kubeconfig: %v", err)
	}

	return path
}

// newBenchClient returns a kubectl client backed by discarded IO streams.
func newBenchClient() *Client {
	return NewClient(genericiooptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	})
}

// BenchmarkCreateClient benchmarks the creation of a new kubectl client.
func BenchmarkCreateClient(b *testing.B) {
	streams := genericiooptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchClientSink = NewClient(streams)
	}
}

// BenchmarkCreateApplyCommand benchmarks the creation of an apply command.
func BenchmarkCreateApplyCommand(b *testing.B) {
	client := newBenchClient()
	kubeconfig := writeBenchKubeconfig(b)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchCommandSink = client.CreateApplyCommand(kubeconfig)
	}
}

// BenchmarkCreateGetCommand benchmarks the creation of a get command.
func BenchmarkCreateGetCommand(b *testing.B) {
	client := newBenchClient()
	kubeconfig := writeBenchKubeconfig(b)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchCommandSink = client.CreateGetCommand(kubeconfig)
	}
}

// BenchmarkCreateDeleteCommand benchmarks the creation of a delete command.
func BenchmarkCreateDeleteCommand(b *testing.B) {
	client := newBenchClient()
	kubeconfig := writeBenchKubeconfig(b)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchCommandSink = client.CreateDeleteCommand(kubeconfig)
	}
}

// BenchmarkCreateDescribeCommand benchmarks the creation of a describe command.
func BenchmarkCreateDescribeCommand(b *testing.B) {
	client := newBenchClient()
	kubeconfig := writeBenchKubeconfig(b)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchCommandSink = client.CreateDescribeCommand(kubeconfig)
	}
}

// BenchmarkCreateLogsCommand benchmarks the creation of a logs command.
func BenchmarkCreateLogsCommand(b *testing.B) {
	client := newBenchClient()
	kubeconfig := writeBenchKubeconfig(b)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchCommandSink = client.CreateLogsCommand(kubeconfig)
	}
}

// BenchmarkCreateWaitCommand benchmarks the creation of a wait command.
func BenchmarkCreateWaitCommand(b *testing.B) {
	client := newBenchClient()
	kubeconfig := writeBenchKubeconfig(b)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		benchCommandSink = client.CreateWaitCommand(kubeconfig)
	}
}

// BenchmarkCreateNamespaceCmd benchmarks the creation of a namespace generator command.
func BenchmarkCreateNamespaceCmd(b *testing.B) {
	client := newBenchClient()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		cmd, err := client.CreateNamespaceCmd()
		if err != nil {
			b.Fatalf("CreateNamespaceCmd failed: %v", err)
		}

		benchCommandSink = cmd
	}
}

// BenchmarkCreateDeploymentCmd benchmarks the creation of a deployment generator command.
func BenchmarkCreateDeploymentCmd(b *testing.B) {
	client := newBenchClient()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		cmd, err := client.CreateDeploymentCmd()
		if err != nil {
			b.Fatalf("CreateDeploymentCmd failed: %v", err)
		}

		benchCommandSink = cmd
	}
}

// BenchmarkCreateServiceCmd benchmarks the creation of a service generator command.
func BenchmarkCreateServiceCmd(b *testing.B) {
	client := newBenchClient()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		cmd, err := client.CreateServiceCmd()
		if err != nil {
			b.Fatalf("CreateServiceCmd failed: %v", err)
		}

		benchCommandSink = cmd
	}
}

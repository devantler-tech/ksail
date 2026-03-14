// Package registryresolver contains benchmarks for the registry resolution hot paths.
// These are white-box benchmarks using the internal package to access unexported types
// and functions that cannot be reached from the external test package.
package registryresolver

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/spf13/viper"
)

// Package-level sinks prevent the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variables are required to prevent compiler optimization.
var (
	benchInfoSink   *Info
	benchStringSink string
	benchErrSink    error
)

// BenchmarkParseOCIURL_LocalhostWithPort measures parsing a local registry OCI URL,
// the most common URL format produced by KSail-managed local registries.
func BenchmarkParseOCIURL_LocalhostWithPort(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink, benchErrSink = parseOCIURL("oci://localhost:5050/myproject")
	}
}

// BenchmarkParseOCIURL_ExternalRegistry measures parsing an external registry OCI URL
// (e.g., ghcr.io) — no port, nested path segment.
func BenchmarkParseOCIURL_ExternalRegistry(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink, benchErrSink = parseOCIURL("oci://ghcr.io/devantler-tech/ksail/config")
	}
}

// BenchmarkParseOCIURL_Empty measures the fast error path when an empty URL is provided.
func BenchmarkParseOCIURL_Empty(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink, benchErrSink = parseOCIURL("")
	}
}

// BenchmarkParseHostPort_WithPort measures parsing a host:port pair — the common
// case for local registries on every registry URL parse.
func BenchmarkParseHostPort_WithPort(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		result := parseHostPort("localhost:5050")
		benchStringSink = result.host
	}
}

// BenchmarkParseHostPort_ExternalNoPort measures parsing an external host without a
// port (e.g., "ghcr.io") — the fallback branch where the suffix is not a valid port.
func BenchmarkParseHostPort_ExternalNoPort(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		result := parseHostPort("ghcr.io")
		benchStringSink = result.host
	}
}

// BenchmarkParseRegistryFlag_Simple measures parsing a plain host:port/path registry flag.
func BenchmarkParseRegistryFlag_Simple(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink = parseRegistryFlag("localhost:5050/myproject")
	}
}

// BenchmarkParseRegistryFlag_WithCredentials measures parsing a registry flag that
// embeds username:password credentials — the common case for external registries.
func BenchmarkParseRegistryFlag_WithCredentials(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink = parseRegistryFlag("user:secret@ghcr.io/devantler-tech/ksail")
	}
}

// BenchmarkFormatRegistryURL_WithPort measures building an OCI URL with an explicit
// port — the common case for local registries.
func BenchmarkFormatRegistryURL_WithPort(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchStringSink = FormatRegistryURL("localhost", 5050, "myproject")
	}
}

// BenchmarkFormatRegistryURL_WithoutPort measures building an OCI URL without a port
// (e.g., external registries like ghcr.io).
func BenchmarkFormatRegistryURL_WithoutPort(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchStringSink = FormatRegistryURL("ghcr.io", 0, "devantler-tech/ksail/config")
	}
}

// BenchmarkDetectRegistryFromViper_Set measures the hot path where a registry value
// is already configured via the --registry flag or KSAIL_REGISTRY env var.
func BenchmarkDetectRegistryFromViper_Set(b *testing.B) {
	v := viper.New()
	v.Set(ViperRegistryKey, "localhost:5050/myproject")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink, benchErrSink = DetectRegistryFromViper(v)
	}
}

// BenchmarkDetectRegistryFromViper_Empty measures the error path when registry is not
// set — Viper returns an empty string and the function returns ErrRegistryNotSet.
func BenchmarkDetectRegistryFromViper_Empty(b *testing.B) {
	v := viper.New()
	// No registry value set — exercises the early-exit error branch.

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink, benchErrSink = DetectRegistryFromViper(v)
	}
}

// BenchmarkDetectRegistryFromConfig_LocalRegistry measures parsing registry info from
// a ksail.yaml config with a local registry — the most common config-based path during
// cluster create and update.
func BenchmarkDetectRegistryFromConfig_LocalRegistry(b *testing.B) {
	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5050",
				},
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink, benchErrSink = DetectRegistryFromConfig(cfg)
	}
}

// BenchmarkDetectRegistryFromConfig_ExternalRegistry measures parsing an external
// registry (e.g., ghcr.io) from config — exercises IsExternal classification and
// credential resolution with the full Parse() path.
func BenchmarkDetectRegistryFromConfig_ExternalRegistry(b *testing.B) {
	cfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "ghcr.io/devantler-tech/ksail",
				},
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchInfoSink, benchErrSink = DetectRegistryFromConfig(cfg)
	}
}

package cluster_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
)

// Package-level sinks prevent the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variables are required to prevent compiler optimization.
var (
	benchSanitizeYAMLSink     string
	benchCountYAMLDocsSink    int
	benchFilterExcludedSink   []string
)

// podYAML is a minimal kubectl-style pod YAML used as benchmark input.
const podYAML = `apiVersion: v1
kind: Pod
metadata:
  name: benchmark-pod
  namespace: default
  resourceVersion: "99999"
  uid: abcd-efgh-ijkl
  selfLink: /api/v1/namespaces/default/pods/benchmark-pod
  creationTimestamp: "2025-01-01T00:00:00Z"
  managedFields:
  - manager: kubectl
    operation: Apply
    apiVersion: v1
  generation: 3
  labels:
    app: benchmark
    env: test
spec:
  containers:
  - name: app
    image: nginx:latest
    ports:
    - containerPort: 80
status:
  phase: Running
  conditions:
  - type: Ready
    status: "True"
`

// podListYAML is a kubectl list response with three pods.
const podListYAML = `apiVersion: v1
kind: PodList
metadata:
  resourceVersion: "12345"
items:
- apiVersion: v1
  kind: Pod
  metadata:
    name: pod-1
    namespace: default
    resourceVersion: "100"
    uid: aaa-bbb-ccc
  spec:
    containers:
    - name: app
      image: nginx:1.25
  status:
    phase: Running
- apiVersion: v1
  kind: Pod
  metadata:
    name: pod-2
    namespace: kube-system
    resourceVersion: "200"
    uid: ddd-eee-fff
  spec:
    containers:
    - name: system
      image: pause:3.9
  status:
    phase: Running
- apiVersion: v1
  kind: Pod
  metadata:
    name: pod-3
    namespace: default
    resourceVersion: "300"
    uid: ggg-hhh-iii
  spec:
    containers:
    - name: worker
      image: busybox:1.36
  status:
    phase: Pending
`

// BenchmarkSanitizeYAMLOutput_SinglePod measures sanitization of a single pod
// manifest — the common case where kubectl returns a single resource.
func BenchmarkSanitizeYAMLOutput_SinglePod(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		result, err := clusterpkg.ExportSanitizeYAMLOutput(podYAML)
		if err != nil {
			b.Fatalf("ExportSanitizeYAMLOutput: %v", err)
		}

		benchSanitizeYAMLSink = result
	}
}

// BenchmarkSanitizeYAMLOutput_PodList measures sanitization of a three-item
// PodList — representative of the list format kubectl returns for most types.
func BenchmarkSanitizeYAMLOutput_PodList(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		result, err := clusterpkg.ExportSanitizeYAMLOutput(podListYAML)
		if err != nil {
			b.Fatalf("ExportSanitizeYAMLOutput: %v", err)
		}

		benchSanitizeYAMLSink = result
	}
}

// BenchmarkSanitizeYAMLOutput_NonYAML measures the fast path for unparseable
// content — the function should return quickly without allocations.
func BenchmarkSanitizeYAMLOutput_NonYAML(b *testing.B) {
	input := "not valid yaml: ["

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		result, err := clusterpkg.ExportSanitizeYAMLOutput(input)
		if err != nil {
			b.Fatalf("ExportSanitizeYAMLOutput: %v", err)
		}

		benchSanitizeYAMLSink = result
	}
}

// BenchmarkCountYAMLDocuments_Single measures counting for a single document.
func BenchmarkCountYAMLDocuments_Single(b *testing.B) {
	content := "kind: Pod\nmetadata:\n  name: test\n"

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchCountYAMLDocsSink = clusterpkg.ExportCountYAMLDocuments(content)
	}
}

// BenchmarkCountYAMLDocuments_List measures counting for a kubectl list output
// with 10 items — represents a typical namespace export.
func BenchmarkCountYAMLDocuments_List(b *testing.B) {
	var sb strings.Builder

	sb.WriteString("apiVersion: v1\nkind: PodList\nmetadata: {}\nitems:\n")

	for i := range 10 {
		sb.WriteString("- apiVersion: v1\n  kind: Pod\n  metadata:\n    name: pod-")
		sb.WriteString(string(rune('0' + i)))
		sb.WriteString("\n")
	}

	content := sb.String()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchCountYAMLDocsSink = clusterpkg.ExportCountYAMLDocuments(content)
	}
}

// BenchmarkFilterExcludedTypes_NoExclusions measures the common case where no
// resource types are excluded.
func BenchmarkFilterExcludedTypes_NoExclusions(b *testing.B) {
	types := []string{
		"customresourcedefinitions", "namespaces", "storageclasses",
		"persistentvolumes", "persistentvolumeclaims", "secrets",
		"configmaps", "serviceaccounts", "roles", "rolebindings",
		"clusterroles", "clusterrolebindings", "services",
		"deployments", "statefulsets", "daemonsets", "jobs",
		"cronjobs", "ingresses",
	}
	exclude := []string{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFilterExcludedSink = clusterpkg.ExportFilterExcludedTypes(types, exclude)
	}
}

// BenchmarkFilterExcludedTypes_DefaultExclusions measures the default case
// where events and pods are excluded — the most common user configuration.
func BenchmarkFilterExcludedTypes_DefaultExclusions(b *testing.B) {
	types := []string{
		"customresourcedefinitions", "namespaces", "storageclasses",
		"persistentvolumes", "persistentvolumeclaims", "secrets",
		"configmaps", "serviceaccounts", "roles", "rolebindings",
		"clusterroles", "clusterrolebindings", "services",
		"deployments", "statefulsets", "daemonsets", "jobs",
		"cronjobs", "ingresses", "events",
	}
	exclude := []string{"events"}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFilterExcludedSink = clusterpkg.ExportFilterExcludedTypes(types, exclude)
	}
}

// BenchmarkCreateTarball_Small measures archive creation for a small backup
// (3 files totaling ~3 KB) — representative of an empty or minimal cluster.
func BenchmarkCreateTarball_Small(b *testing.B) {
	srcDir := b.TempDir()
	setupBenchmarkFiles(b, srcDir, 3, 1024)

	outDir := b.TempDir()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := filepath.Join(outDir, "backup.tar.gz")

		if err := clusterpkg.ExportCreateTarball(srcDir, out, 6); err != nil {
			b.Fatalf("ExportCreateTarball: %v", err)
		}
	}
}

// BenchmarkCreateTarball_Medium measures archive creation for a medium backup
// (20 files totaling ~20 KB) — representative of a typical development cluster.
func BenchmarkCreateTarball_Medium(b *testing.B) {
	srcDir := b.TempDir()
	setupBenchmarkFiles(b, srcDir, 20, 1024)

	outDir := b.TempDir()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := filepath.Join(outDir, "backup.tar.gz")

		if err := clusterpkg.ExportCreateTarball(srcDir, out, 6); err != nil {
			b.Fatalf("ExportCreateTarball: %v", err)
		}
	}
}

// setupBenchmarkFiles populates dir with count files each of size bytes,
// simulating the resource YAML files written by exportResources.
func setupBenchmarkFiles(b *testing.B, dir string, count, size int) {
	b.Helper()

	payload := bytes.Repeat([]byte("x"), size)

	for i := range count {
		subDir := filepath.Join(dir, "resources", "type"+string(rune('a'+i%26)))

		if err := os.MkdirAll(subDir, clusterpkg.ExportDirPerm); err != nil {
			b.Fatalf("setup: mkdir: %v", err)
		}

		if err := os.WriteFile(
			filepath.Join(subDir, "resource.yaml"), payload, clusterpkg.ExportFilePerm,
		); err != nil {
			b.Fatalf("setup: write file: %v", err)
		}
	}
}

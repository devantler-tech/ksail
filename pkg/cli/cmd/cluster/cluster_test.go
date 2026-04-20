package cluster_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Package-level sinks prevent the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variables are required to prevent compiler optimization.
var (
	benchSanitizeYAMLSink   string
	benchCountYAMLDocsSink  int
	benchFilterExcludedSink []string
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
		result, err := cluster.ExportSanitizeYAMLOutput(podYAML)
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
		result, err := cluster.ExportSanitizeYAMLOutput(podListYAML)
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
		result, err := cluster.ExportSanitizeYAMLOutput(input)
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
		benchCountYAMLDocsSink = cluster.ExportCountYAMLDocuments(content)
	}
}

// BenchmarkCountYAMLDocuments_List measures counting for a kubectl list output
// with 10 items — represents a typical namespace export.
func BenchmarkCountYAMLDocuments_List(b *testing.B) {
	var builder strings.Builder

	builder.WriteString("apiVersion: v1\nkind: PodList\nmetadata: {}\nitems:\n")

	for i := range 10 {
		builder.WriteString("- apiVersion: v1\n  kind: Pod\n  metadata:\n    name: pod-")
		builder.WriteRune(rune('0' + i))
		builder.WriteString("\n")
	}

	content := builder.String()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchCountYAMLDocsSink = cluster.ExportCountYAMLDocuments(content)
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
		benchFilterExcludedSink = cluster.ExportFilterExcludedTypes(types, exclude)
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
		benchFilterExcludedSink = cluster.ExportFilterExcludedTypes(types, exclude)
	}
}

// BenchmarkCreateTarball_Small measures archive creation for a small backup
// (3 files totaling ~3 KB) — representative of an empty or minimal cluster.
func BenchmarkCreateTarball_Small(b *testing.B) {
	srcDir := b.TempDir()
	setupBenchmarkFiles(b, srcDir, 3, 1024)

	outDir := b.TempDir()

	// Warm the OS page cache so the first timed iteration doesn't incur a
	// cold-cache penalty on shared CI runners (see #4090).
	warmupOut := filepath.Join(outDir, "warmup.tar.gz")

	err := cluster.ExportCreateTarball(srcDir, warmupOut, 6)
	if err != nil {
		b.Fatalf("warmup: %v", err)
	}

	_ = os.Remove(warmupOut)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := filepath.Join(outDir, "backup.tar.gz")

		err := cluster.ExportCreateTarball(srcDir, out, 6)
		if err != nil {
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

	// Warm the OS page cache so the first timed iteration doesn't incur a
	// cold-cache penalty on shared CI runners (see #4090).
	warmupOut := filepath.Join(outDir, "warmup.tar.gz")

	err := cluster.ExportCreateTarball(srcDir, warmupOut, 6)
	if err != nil {
		b.Fatalf("warmup: %v", err)
	}

	_ = os.Remove(warmupOut)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		out := filepath.Join(outDir, "backup.tar.gz")

		err := cluster.ExportCreateTarball(srcDir, out, 6)
		if err != nil {
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

		err := os.MkdirAll(subDir, cluster.ExportDirPerm)
		if err != nil {
			b.Fatalf("setup: mkdir: %v", err)
		}

		err = os.WriteFile(
			filepath.Join(subDir, "resource.yaml"), payload, cluster.ExportFilePerm,
		)
		if err != nil {
			b.Fatalf("setup: write file: %v", err)
		}
	}
}

func TestBackupMetadata(t *testing.T) {
	t.Parallel()

	metadata := &cluster.BackupMetadata{
		Version:       "v1",
		ClusterName:   "test-cluster",
		Distribution:  "Vanilla",
		Provider:      "Docker",
		KSailVersion:  "5.0.0",
		ResourceCount: 42,
		ResourceTypes: []string{"deployments", "services"},
	}

	tmpDir := t.TempDir()
	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")

	err := cluster.ExportWriteMetadata(metadata, metadataPath)
	if err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	_, statErr := os.Stat(metadataPath)
	if os.IsNotExist(statErr) {
		t.Fatal("metadata file was not created")
	}

	data, err := os.ReadFile(metadataPath) //nolint:gosec // test-controlled path from t.TempDir()
	if err != nil {
		t.Fatalf("failed to read metadata file: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("metadata file is empty")
	}

	content := string(data)
	for _, field := range []string{
		`"distribution"`, `"provider"`, `"resourceTypes"`,
	} {
		if !strings.Contains(content, field) {
			t.Errorf("metadata file should contain %s", field)
		}
	}
}

func TestCreateTarball(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()

	testFile := filepath.Join(srcDir, "test.txt")

	err := os.WriteFile(testFile, []byte("test content"), cluster.ExportFilePerm)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	subDir := filepath.Join(srcDir, "subdir")

	err = os.MkdirAll(subDir, cluster.ExportDirPerm)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	subFile := filepath.Join(subDir, "sub.txt")

	err = os.WriteFile(subFile, []byte("sub content"), cluster.ExportFilePerm)
	if err != nil {
		t.Fatalf("failed to create sub file: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "test-backup.tar.gz")

	err = cluster.ExportCreateTarball(srcDir, outputPath, 6)
	if err != nil {
		t.Fatalf("failed to create tarball: %v", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("failed to stat tarball: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("tarball is empty")
	}
}

func TestCountYAMLDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "single document",
			content:  "kind: Pod\nmetadata:\n  name: test",
			expected: 1,
		},
		{
			name:     "multiple documents",
			content:  "kind: Pod\n---\nkind: Service\n---\nkind: Deployment",
			expected: 3,
		},
		{
			name:     "no kind lines returns 1",
			content:  "metadata:\n  name: test",
			expected: 1,
		},
		{
			name: "kubectl list output",
			content: "apiVersion: v1\nkind: PodList\nmetadata:\n" +
				"items:\n- apiVersion: v1\n  kind: Pod\n  metadata:\n" +
				"    name: pod1\n- apiVersion: v1\n  kind: Pod\n  metadata:\n" +
				"    name: pod2\n",
			expected: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			count := cluster.ExportCountYAMLDocuments(test.content)
			if count != test.expected {
				t.Errorf(
					"countYAMLDocuments() = %d, want %d",
					count, test.expected,
				)
			}
		})
	}
}

func TestFilterExcludedTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		types       []string
		exclude     []string
		expectedLen int
	}{
		{
			name:        "no exclusions",
			types:       []string{"pods", "services", "deployments"},
			exclude:     []string{},
			expectedLen: 3,
		},
		{
			name:        "exclude one",
			types:       []string{"pods", "services", "deployments"},
			exclude:     []string{"pods"},
			expectedLen: 2,
		},
		{
			name:        "exclude all",
			types:       []string{"pods"},
			exclude:     []string{"pods"},
			expectedLen: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportFilterExcludedTypes(test.types, test.exclude)
			if len(result) != test.expectedLen {
				t.Errorf(
					"filterExcludedTypes() returned %d items, want %d",
					len(result), test.expectedLen,
				)
			}
		})
	}
}

func TestExtractAndReadMetadata(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	metadata := &cluster.BackupMetadata{
		Version:       "v1",
		ClusterName:   "roundtrip-cluster",
		Distribution:  "K3s",
		Provider:      "Docker",
		KSailVersion:  "5.0.0",
		ResourceCount: 10,
		ResourceTypes: []string{"deployments", "services"},
	}

	metadataPath := filepath.Join(srcDir, "backup-metadata.json")

	err := cluster.ExportWriteMetadata(metadata, metadataPath)
	if err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "test.tar.gz")

	err = cluster.ExportCreateTarball(srcDir, archivePath, 6)
	if err != nil {
		t.Fatalf("failed to create tarball: %v", err)
	}

	tmpDir, restored, err := cluster.ExportExtractBackupArchive(archivePath)
	if err != nil {
		t.Fatalf("failed to extract backup archive: %v", err)
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	assertMetadataRoundtrip(t, restored)
}

func assertMetadataRoundtrip(t *testing.T, restored *cluster.BackupMetadata) {
	t.Helper()

	if restored.Version != "v1" {
		t.Errorf("Version = %q, want %q", restored.Version, "v1")
	}

	if restored.ClusterName != "roundtrip-cluster" {
		t.Errorf("ClusterName = %q, want %q", restored.ClusterName, "roundtrip-cluster")
	}

	if restored.ResourceCount != 10 {
		t.Errorf("ResourceCount = %d, want %d", restored.ResourceCount, 10)
	}

	if restored.Distribution != "K3s" {
		t.Errorf("Distribution = %q, want %q", restored.Distribution, "K3s")
	}

	if restored.Provider != "Docker" {
		t.Errorf("Provider = %q, want %q", restored.Provider, "Docker")
	}

	if len(restored.ResourceTypes) != 2 {
		t.Errorf("ResourceTypes length = %d, want %d", len(restored.ResourceTypes), 2)
	}
}

func TestSanitizeYAMLOutput(t *testing.T) {
	t.Parallel()

	input := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test-pod\n" +
		"  namespace: default\n  resourceVersion: \"12345\"\n" +
		"  uid: abc-123\n  managedFields:\n  - manager: kubectl\n" +
		"  creationTimestamp: \"2025-01-01T00:00:00Z\"\n" +
		"status:\n  phase: Running\nspec:\n  containers:\n  - name: nginx"

	result, err := cluster.ExportSanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	for _, stripped := range []string{
		"resourceVersion", "uid", "managedFields",
		"creationTimestamp", "status",
	} {
		if strings.Contains(result, stripped) {
			t.Errorf("should have stripped %q", stripped)
		}
	}

	for _, preserved := range []string{
		"name: test-pod", "namespace: default",
		"kind: Pod", "apiVersion: v1",
	} {
		if !strings.Contains(result, preserved) {
			t.Errorf("should preserve %q", preserved)
		}
	}
}

func TestSanitizeYAMLOutput_nonYAML(t *testing.T) {
	t.Parallel()

	result, err := cluster.ExportSanitizeYAMLOutput("not valid yaml: [")
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	if !strings.Contains(result, "not valid yaml") {
		t.Error("should return original content for non-YAML input")
	}
}

func TestSanitizeYAMLOutput_stripsLastAppliedConfiguration(t *testing.T) {
	t.Parallel()

	input := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test-cm\n" +
		"  annotations:\n    kubectl.kubernetes.io/last-applied-configuration: " +
		"'{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}'\n" +
		"    custom-annotation: keep-me\n" +
		"data:\n  key: value"

	result, err := cluster.ExportSanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	if strings.Contains(result, "last-applied-configuration") {
		t.Error("should have stripped kubectl.kubernetes.io/last-applied-configuration")
	}

	if !strings.Contains(result, "custom-annotation") {
		t.Error("should preserve other annotations")
	}
}

func TestSanitizeYAMLOutput_filtersHelmReleaseSecrets(t *testing.T) {
	t.Parallel()

	input := `apiVersion: v1
kind: SecretList
metadata: {}
items:
- apiVersion: v1
  kind: Secret
  metadata:
    name: sh.helm.release.v1.argocd.v1
    namespace: argocd
  type: helm.sh/release.v1
  data:
    release: dGVzdA==
- apiVersion: v1
  kind: Secret
  metadata:
    name: my-app-secret
    namespace: default
  type: Opaque
  data:
    password: cGFzc3dvcmQ=`

	result, err := cluster.ExportSanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	if strings.Contains(result, "sh.helm.release.v1") {
		t.Error("should have filtered out Helm release secret")
	}

	if !strings.Contains(result, "my-app-secret") {
		t.Error("should preserve non-Helm secrets")
	}
}

func TestSanitizeYAMLOutput_filtersSingleHelmReleaseSecret(t *testing.T) {
	t.Parallel()

	input := `apiVersion: v1
kind: Secret
metadata:
  name: sh.helm.release.v1.argocd.v1
  namespace: argocd
type: helm.sh/release.v1
data:
  release: dGVzdA==`

	result, err := cluster.ExportSanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	if result != "" {
		t.Errorf("single Helm release Secret should be filtered to empty, got: %q", result)
	}
}

func TestSanitizeYAMLOutput_stripsServiceClusterIPs(t *testing.T) {
	t.Parallel()

	input := `apiVersion: v1
kind: Service
metadata:
  name: my-service
  namespace: default
spec:
  clusterIP: 10.96.0.100
  clusterIPs:
  - 10.96.0.100
  ports:
  - port: 80
    targetPort: 8080
  selector:
    app: my-app
  type: ClusterIP`

	result, err := cluster.ExportSanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	if strings.Contains(result, "clusterIP:") {
		t.Error("should strip spec.clusterIP from Service")
	}

	if strings.Contains(result, "clusterIPs:") {
		t.Error("should strip spec.clusterIPs from Service")
	}

	if !strings.Contains(result, "my-service") {
		t.Error("should preserve service name")
	}

	if !strings.Contains(result, "targetPort") {
		t.Error("should preserve other spec fields")
	}
}

func TestSanitizeYAMLOutput_preservesHeadlessService(t *testing.T) {
	t.Parallel()

	input := `apiVersion: v1
kind: Service
metadata:
  name: headless-svc
  namespace: default
spec:
  clusterIP: None
  ports:
  - port: 80
  selector:
    app: my-app`

	result, err := cluster.ExportSanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("sanitizeYAMLOutput() error = %v", err)
	}

	if !strings.Contains(result, "clusterIP") {
		t.Error("should preserve clusterIP: None for headless services")
	}

	if !strings.Contains(result, "clusterIP: None") {
		t.Error("should preserve clusterIP: None for headless services")
	}
}

func TestIsHelmReleaseSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     string
		objType  string
		expected bool
	}{
		{
			name:     "helm release secret",
			kind:     "Secret",
			objType:  "helm.sh/release.v1",
			expected: true,
		},
		{
			name:     "opaque secret",
			kind:     "Secret",
			objType:  "Opaque",
			expected: false,
		},
		{
			name:     "non-secret resource",
			kind:     "ConfigMap",
			objType:  "",
			expected: false,
		},
		{
			name:     "secret without type",
			kind:     "Secret",
			objType:  "",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "v1",
					"kind":       test.kind,
					"metadata":   map[string]any{"name": "test"},
				},
			}
			if test.objType != "" {
				obj.Object["type"] = test.objType
			}

			result := cluster.ExportIsHelmReleaseSecret(obj)
			if result != test.expected {
				t.Errorf(
					"isHelmReleaseSecret() = %v, want %v",
					result, test.expected,
				)
			}
		})
	}
}

type validateTarEntryTest struct {
	name    string
	header  *tar.Header
	wantErr bool
	errType error
}

func pathTraversalTestCases() []validateTarEntryTest {
	return []validateTarEntryTest{
		{
			name:   "valid regular file",
			header: &tar.Header{Name: "resources/pods.yaml", Typeflag: tar.TypeReg},
		},
		{
			name:   "valid directory",
			header: &tar.Header{Name: "resources/", Typeflag: tar.TypeDir},
		},
		{
			name:    "absolute path",
			header:  &tar.Header{Name: "/etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "parent directory traversal",
			header:  &tar.Header{Name: "../../../etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "embedded parent traversal",
			header:  &tar.Header{Name: "resources/../../etc/passwd", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "double dot only",
			header:  &tar.Header{Name: "..", Typeflag: tar.TypeReg},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
	}
}

func specialTypeTestCases() []validateTarEntryTest {
	return []validateTarEntryTest{
		{
			name: "symlink",
			header: &tar.Header{
				Name:     "link.yaml",
				Typeflag: tar.TypeSymlink,
				Linkname: "/etc/passwd",
			},
			wantErr: true, errType: cluster.ErrSymlinkInArchive,
		},
		{
			name:    "hard link",
			header:  &tar.Header{Name: "link.yaml", Typeflag: tar.TypeLink, Linkname: "other.yaml"},
			wantErr: true, errType: cluster.ErrSymlinkInArchive,
		},
		{
			name:    "char device",
			header:  &tar.Header{Name: "dev", Typeflag: tar.TypeChar},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "block device",
			header:  &tar.Header{Name: "dev", Typeflag: tar.TypeBlock},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
		{
			name:    "FIFO",
			header:  &tar.Header{Name: "fifo", Typeflag: tar.TypeFifo},
			wantErr: true, errType: cluster.ErrInvalidTarPath,
		},
	}
}

func runValidateTarEntryTests(
	t *testing.T, destDir string, tests []validateTarEntryTest,
) {
	t.Helper()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := cluster.ExportValidateTarEntry(test.header, destDir)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				if test.errType != nil &&
					!errors.Is(err, test.errType) {
					t.Errorf(
						"expected error wrapping %v, got %v",
						test.errType, err,
					)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateTarEntry(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()

	runValidateTarEntryTests(t, destDir, pathTraversalTestCases())
	runValidateTarEntryTests(t, destDir, specialTypeTestCases())
}

func TestAllLinesContain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		substr   string
		expected bool
	}{
		{
			name:     "all lines match",
			output:   "error: already exists\nerror: already exists\n",
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "one line does not match",
			output:   "error: already exists\nerror: forbidden\n",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "empty string",
			output:   "",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "whitespace only",
			output:   "   \n  \n",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "single matching line",
			output:   "resource already exists",
			substr:   "already exists",
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportAllLinesContain(
				test.output, test.substr,
			)
			if result != test.expected {
				t.Errorf(
					"allLinesContain() = %v, want %v",
					result, test.expected,
				)
			}
		})
	}
}

func TestDeriveBackupName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tar.gz extension",
			input:    "/path/to/my-backup.tar.gz",
			expected: "my-backup",
		},
		{
			name:     "tgz extension",
			input:    "/path/to/backup.tgz",
			expected: "backup",
		},
		{
			name:     "no matching extension",
			input:    "/path/to/backup.zip",
			expected: "backup.zip",
		},
		{
			name:     "simple filename",
			input:    "cluster-backup.tar.gz",
			expected: "cluster-backup",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportDeriveBackupName(test.input)
			if result != test.expected {
				t.Errorf(
					"deriveBackupName() = %q, want %q",
					result, test.expected,
				)
			}
		})
	}
}

func TestAddLabelsToDocument(t *testing.T) {
	t.Parallel()

	for _, test := range addLabelsTestCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := cluster.ExportAddLabelsToDocument(
				test.doc, test.backupName, test.restoreName,
			)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, label := range test.wantLabels {
				if !strings.Contains(result, label) {
					t.Errorf("result should contain label %q", label)
				}
			}
		})
	}
}

type addLabelsTestCase struct {
	name        string
	doc         string
	backupName  string
	restoreName string
	wantLabels  []string
	wantErr     bool
}

func addLabelsTestCases() []addLabelsTestCase {
	return []addLabelsTestCase{
		{
			name: "adds labels to document without existing labels",
			doc: "apiVersion: v1\nkind: Pod\nmetadata:\n" +
				"  name: test-pod\n  namespace: default\n",
			backupName:  "my-backup",
			restoreName: "restore-123",
			wantLabels: []string{
				"ksail.io/backup-name",
				"ksail.io/restore-name",
			},
		},
		{
			name: "preserves existing labels",
			doc: "apiVersion: v1\nkind: Pod\nmetadata:\n" +
				"  name: test-pod\n  labels:\n    app: nginx\n",
			backupName:  "backup-1",
			restoreName: "restore-1",
			wantLabels: []string{
				"app",
				"ksail.io/backup-name",
				"ksail.io/restore-name",
			},
		},
		{
			name:        "returns original for empty doc",
			doc:         "\n",
			backupName:  "backup",
			restoreName: "restore",
			wantErr:     false,
		},
	}
}

func TestSplitYAMLDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{
			name:     "single document",
			content:  "apiVersion: v1\nkind: Pod\n",
			expected: 1,
		},
		{
			name:     "two documents",
			content:  "apiVersion: v1\nkind: Pod\n---\napiVersion: v1\nkind: Service\n",
			expected: 2,
		},
		{
			name:     "three documents",
			content:  "kind: A\n---\nkind: B\n---\nkind: C\n",
			expected: 3,
		},
		{
			name:     "empty content",
			content:  "",
			expected: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportSplitYAMLDocuments(test.content)
			if len(result) != test.expected {
				t.Errorf(
					"splitYAMLDocuments() returned %d docs, want %d",
					len(result), test.expected,
				)
			}
		})
	}
}

func TestInjectRestoreLabels(t *testing.T) {
	t.Parallel()

	content := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test\n"

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "test.yaml")

	err := os.WriteFile(inputPath, []byte(content), cluster.ExportFilePerm)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	labeledPath, err := cluster.ExportInjectRestoreLabels(
		inputPath, "my-backup", "restore-42",
	)
	if err != nil {
		t.Fatalf("injectRestoreLabels() error: %v", err)
	}

	defer func() { _ = os.Remove(labeledPath) }()

	data, err := os.ReadFile(labeledPath) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("failed to read labeled file: %v", err)
	}

	result := string(data)

	if !strings.Contains(result, "ksail.io/backup-name") {
		t.Error("labeled file should contain ksail.io/backup-name")
	}

	if !strings.Contains(result, "ksail.io/restore-name") {
		t.Error("labeled file should contain ksail.io/restore-name")
	}

	if !strings.Contains(result, "my-backup") {
		t.Error("labeled file should contain backup name value")
	}

	if !strings.Contains(result, "restore-42") {
		t.Error("labeled file should contain restore name value")
	}
}

func TestCreateTarball_OverwritesExistingTarget(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()

	err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("data"), cluster.ExportFilePerm)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "backup.tar.gz")

	// First run creates the archive.
	err = cluster.ExportCreateTarball(srcDir, outputPath, -1)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run to the same path must succeed (overwrite) without error.
	err = cluster.ExportCreateTarball(srcDir, outputPath, -1)
	if err != nil {
		t.Fatalf("second run (overwrite): %v", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil || info.Size() == 0 {
		t.Fatal("archive not present or empty after overwrite")
	}
}

func TestCreateTarball_NoTempFileLeftOnSourceDirError(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "backup.tar.gz")

	// Use a guaranteed-nonexistent directory under t.TempDir() so the test is
	// deterministic across all environments (avoids /nonexistent-... paths that
	// could theoretically exist on some systems).
	nonexistentDir := filepath.Join(t.TempDir(), "does-not-exist")

	err := cluster.ExportCreateTarball(nonexistentDir, outputPath, -1)
	if err == nil {
		t.Fatal("expected error for non-existent source dir, got nil")
	}

	// No .tmp* files should remain in the output directory.
	entries, readErr := os.ReadDir(outputDir)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}

	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestCreateTarball_SkipsSymlinks(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()

	realFile := filepath.Join(srcDir, "real.txt")

	err := os.WriteFile(realFile, []byte("data"), cluster.ExportFilePerm)
	if err != nil {
		t.Fatalf("setup real file: %v", err)
	}

	linkPath := filepath.Join(srcDir, "link.txt")

	err = os.Symlink(realFile, linkPath)
	if err != nil {
		// Symlinks may be unsupported on this platform/OS configuration.
		t.Skipf("skipping: os.Symlink not supported: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "backup.tar.gz")

	err = cluster.ExportCreateTarball(srcDir, outputPath, -1)
	if err != nil {
		t.Fatalf("createTarball: %v", err)
	}

	// Read back the archive and verify no symlink entry is present.
	archiveFile, err := os.Open( //nolint:gosec // G304: test-generated path in t.TempDir()
		outputPath,
	)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}

	defer func() { _ = archiveFile.Close() }()

	gzReader, err := gzip.NewReader(archiveFile)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}

	defer func() { _ = gzReader.Close() }()

	tr := tar.NewReader(gzReader)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}

		if hdr.Name == "link.txt" {
			t.Errorf("symlink entry %q must not appear in the archive", hdr.Name)
		}
	}
}

type fakeProvisioner struct{}

func (*fakeProvisioner) Create(context.Context, string) error { return nil }
func (*fakeProvisioner) Delete(context.Context, string) error { return nil }
func (*fakeProvisioner) Start(context.Context, string) error  { return nil }
func (*fakeProvisioner) Stop(context.Context, string) error   { return nil }
func (*fakeProvisioner) List(context.Context) ([]string, error) {
	return nil, nil
}
func (*fakeProvisioner) Exists(context.Context, string) (bool, error) { return true, nil }

type fakeFactory struct{}

func (fakeFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeProvisioner{}, cfg, nil
}

type fakeInstaller struct{ called bool }

func (f *fakeInstaller) Install(context.Context) error {
	f.called = true

	return nil
}

func (*fakeInstaller) Uninstall(context.Context) error { return nil }

func (*fakeInstaller) Images(context.Context) ([]string, error) { return nil, nil }

// fakeRegistryService is a mock registry service for testing.
type fakeRegistryService struct{}

func (*fakeRegistryService) Create(
	_ context.Context,
	_ registry.CreateOptions,
) (v1alpha1.OCIRegistry, error) {
	return v1alpha1.NewOCIRegistry(), nil
}

func (*fakeRegistryService) Start(
	_ context.Context,
	_ registry.StartOptions,
) (v1alpha1.OCIRegistry, error) {
	return v1alpha1.NewOCIRegistry(), nil
}

func (*fakeRegistryService) Stop(_ context.Context, _ registry.StopOptions) error {
	return nil
}

func (*fakeRegistryService) Status(
	_ context.Context,
	_ registry.StatusOptions,
) (v1alpha1.OCIRegistry, error) {
	return v1alpha1.NewOCIRegistry(), nil
}

func fakeRegistryServiceFactory(_ registry.Config) (registry.Service, error) {
	return &fakeRegistryService{}, nil
}

// newMockDockerClient creates a mock Docker API client for use in tests.
// It stubs all commonly-used Docker operations to succeed as no-ops.
func newMockDockerClient(t *testing.T) *dockerpkg.MockAPIClient {
	t.Helper()

	mockClient := dockerpkg.NewMockAPIClient(t)

	// Network operations - return empty/success
	mockClient.EXPECT().
		NetworkList(mock.Anything, mock.Anything).
		Return([]network.Summary{}, nil).Maybe()
	mockClient.EXPECT().
		NetworkCreate(mock.Anything, mock.Anything, mock.Anything).
		Return(network.CreateResponse{}, nil).Maybe()
	mockClient.EXPECT().
		NetworkRemove(mock.Anything, mock.Anything).
		Return(nil).Maybe()
	mockClient.EXPECT().
		NetworkConnect(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()

	// Container operations - return empty list (no existing containers)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).
		Maybe()

	// Close operation - succeed
	mockClient.EXPECT().Close().Return(nil).Maybe()

	return mockClient
}

// setupMockRegistryBackend configures a mock registry backend that doesn't create real containers.
// Call this in tests to enable default mirror registries (docker.io, ghcr.io) without Docker.
// This also mocks the Docker client invoker to use a mock Docker API client.
//
// IMPORTANT: Call this BEFORE other test setup helpers (like setupGitOpsTestMocks) to ensure
// the mock Docker client is properly configured for all Docker operations.
func setupMockRegistryBackend(t *testing.T) {
	t.Helper()

	mockBackend := registry.NewMockBackend(t)
	// Allow any calls to ListRegistries - returns empty list (no existing registries)
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{}, nil).Maybe()
	// Allow any calls to GetRegistryPort - returns 0, not found (no existing registries)
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, mock.Anything).Return(0, nil).Maybe()
	// Allow any calls to CreateRegistry - succeeds (no-op in tests)
	mockBackend.EXPECT().CreateRegistry(mock.Anything, mock.Anything).Return(nil).Maybe()
	// Allow any calls to DeleteRegistry - succeeds (no-op in tests)
	mockBackend.EXPECT().
		DeleteRegistry(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).
		Maybe()
	// Allow any calls to WaitForRegistriesReady - succeeds immediately (no-op in tests)
	mockBackend.EXPECT().WaitForRegistriesReady(mock.Anything, mock.Anything).Return(nil).Maybe()

	t.Cleanup(registry.SetBackendFactoryForTests(
		func(_ client.APIClient) (registry.Backend, error) {
			return mockBackend, nil
		},
	))

	// Mock the Docker client invoker to use a mock Docker API client.
	// This calls the callback with a mock client so stages execute and print output.
	t.Cleanup(cluster.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, fn func(client.APIClient) error) error {
			mockClient := newMockDockerClient(t)

			return fn(mockClient)
		},
	))

	// Override the cluster stability check to be a no-op.
	// Tests use a fake kubeconfig without a real cluster, so the real
	// stability check would time out waiting for API server connectivity.
	t.Cleanup(setup.SetClusterStabilityCheckForTests(
		func(_ context.Context, _ *v1alpha1.Cluster, _ bool) error {
			return nil
		},
	))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// writeTestConfigFiles writes test config files with local registry disabled.
// This produces minimal output without needing Docker client mocking.
func writeTestConfigFiles(t *testing.T, workingDir string) {
	t.Helper()

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    metricsServer: Disabled
    localRegistry:
      enabled: false
    connection:
      kubeconfig: ./kubeconfig
`

	writeFile(t, workingDir, "ksail.yaml", ksailYAML)
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n",
	)
	// Create a fake kubeconfig file with the expected context entry to prevent
	// validation errors when ArgoCD tries to create a Helm client.
	// Vanilla distribution with kind.yaml name "test" → context "kind-test".
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\ncurrent-context: kind-test\nclusters:\n"+
			"- cluster:\n    server: https://127.0.0.1:6443\n  name: kind-test\n"+
			"contexts:\n- context:\n    cluster: kind-test\n    user: kind-test\n  name: kind-test\n"+
			"users:\n- name: kind-test\n  user:\n    token: fake\n",
	)
}

func newTestRuntimeContainer(t *testing.T) *di.Runtime {
	t.Helper()

	return di.New(
		func(i di.Injector) error {
			do.Provide(i, func(di.Injector) (timer.Timer, error) {
				return timer.New(), nil
			})

			return nil
		},
		func(i di.Injector) error {
			do.Provide(i, func(di.Injector) (clusterprovisioner.Factory, error) {
				return fakeFactory{}, nil
			})

			return nil
		},
	)
}

// trimTrailingNewline removes a single trailing newline from snapshot output.
// This produces cleaner snapshot comparisons.
func trimTrailingNewline(s string) string {
	return strings.TrimSuffix(s, "\n")
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EnabledCertManager_PrintsInstallStage(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	fake := &fakeInstaller{}

	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fake, nil
		},
	)
	defer restore()

	testRuntime := newTestRuntimeContainer(t)

	cmd := cluster.NewCreateCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cert-manager", "Enabled"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if !fake.called {
		t.Fatalf("expected cert-manager installer to be invoked")
	}

	// Normalize timing variance: keep --timing disabled in this test.
	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_DefaultCertManager_DoesNotInstall(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	factoryCalled := false

	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			factoryCalled = true

			return &fakeInstaller{}, nil
		},
	)
	defer restore()

	cmd := cluster.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if factoryCalled {
		t.Fatalf("expected cert-manager installer factory not to be invoked")
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

func setupGitOpsTestMocks(
	t *testing.T,
	engine v1alpha1.GitOpsEngine,
) (func() *fakeInstaller, *bool) {
	t.Helper()

	var fake *fakeInstaller

	ensureCalled := false

	// Override cluster provisioner factory to use fake provisioner
	t.Cleanup(cluster.SetProvisionerFactoryForTests(fakeFactory{}))

	// Set up the appropriate installer and ensure mocks based on the GitOps engine
	switch engine {
	case v1alpha1.GitOpsEngineArgoCD:
		setupArgoCDMocks(t, &fake, &ensureCalled)
	case v1alpha1.GitOpsEngineFlux:
		setupFluxMocks(t, &fake, &ensureCalled)
	case v1alpha1.GitOpsEngineNone:
		t.Fatalf("GitOpsEngineNone is not supported in this test helper")
	}

	// Mock registry service factory to avoid needing a real Docker client
	t.Cleanup(cluster.SetLocalRegistryServiceFactoryForTests(fakeRegistryServiceFactory))

	// Note: DockerClientInvoker is NOT overridden here - tests should call
	// setupMockRegistryBackend() before setupGitOpsTestMocks() to configure
	// a mock Docker client that will be used for all Docker operations.

	return func() *fakeInstaller { return fake }, &ensureCalled
}

func setupArgoCDMocks(t *testing.T, fake **fakeInstaller, ensureCalled *bool) {
	t.Helper()
	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			*fake = &fakeInstaller{}

			return *fake, nil
		},
	))
	t.Cleanup(cluster.SetEnsureArgoCDResourcesForTests(
		func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string) error {
			*ensureCalled = true

			return nil
		},
	))
	t.Cleanup(cluster.SetEnsureOCIArtifactForTests(
		func(_ context.Context, _ *cobra.Command, _ *v1alpha1.Cluster, _ string, _ io.Writer) (bool, error) {
			return true, nil
		},
	))
}

func setupFluxMocks(t *testing.T, fake **fakeInstaller, ensureCalled *bool) {
	t.Helper()
	t.Cleanup(cluster.SetFluxInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			*fake = &fakeInstaller{}

			return *fake, nil
		},
	))
	t.Cleanup(cluster.SetSetupFluxInstanceForTests(
		func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string, _ string) error {
			*ensureCalled = true

			return nil
		},
	))
	t.Cleanup(cluster.SetWaitForFluxReadyForTests(
		func(_ context.Context, _ string) error {
			return nil
		},
	))
	t.Cleanup(cluster.SetEnsureOCIArtifactForTests(
		func(_ context.Context, _ *cobra.Command, _ *v1alpha1.Cluster, _ string, _ io.Writer) (bool, error) {
			return true, nil
		},
	))
}

func TestCreate_GitOps_PrintsInstallStage(t *testing.T) {
	testCases := []struct {
		name   string
		engine v1alpha1.GitOpsEngine
		arg    string
	}{
		{name: "ArgoCD", engine: v1alpha1.GitOpsEngineArgoCD, arg: "ArgoCD"},
		{name: "Flux", engine: v1alpha1.GitOpsEngineFlux, arg: "Flux"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tmpRoot := t.TempDir()
			t.Setenv("TMPDIR", tmpRoot)

			workingDir := t.TempDir()
			t.Chdir(workingDir)
			writeTestConfigFiles(t, workingDir)
			setupMockRegistryBackend(t)

			fake, ensureCalled := setupGitOpsTestMocks(t, testCase.engine)

			testRuntime := newTestRuntimeContainer(t)

			cmd := cluster.NewCreateCmd(testRuntime)

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())
			cmd.SetArgs([]string{"--gitops-engine", testCase.arg})

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
			}

			if !*ensureCalled {
				t.Fatalf("expected %s resources ensure hook to be invoked", testCase.name)
			}

			if installer := fake(); installer == nil || !installer.called {
				t.Fatalf("expected %s installer to be invoked", testCase.name)
			}

			snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
		})
	}
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_CSIEnabled_InstallsOnKind(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    csi: Enabled
    metricsServer: Disabled
    connection:
      kubeconfig: ./kubeconfig
`
	writeFile(t, workingDir, "ksail.yaml", ksailYAML)
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n",
	)
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	fake := &fakeInstaller{}

	restore := cluster.SetCSIInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fake, nil
		},
	)
	defer restore()

	setupMockRegistryBackend(t)

	cmd := cluster.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if !fake.called {
		t.Fatalf("expected CSI installer to be invoked")
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_DefaultCSI_DoesNotInstall(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

// TestCreate_Minimal_PrintsOnlyClusterLifecycle tests cluster creation with no extras.
// This verifies the minimal output when all optional components are disabled.
// Uses config with localRegistry disabled to skip registry stages.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_Minimal_PrintsOnlyClusterLifecycle(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir) // Uses config with localRegistry disabled
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

// TestCreate_LocalRegistryDisabled_SkipsRegistryStages tests cluster creation with local registry disabled.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_LocalRegistryDisabled_SkipsRegistryStages(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    localRegistry:
      enabled: false
    metricsServer: Disabled
    connection:
      kubeconfig: ./kubeconfig
`
	writeFile(t, workingDir, "ksail.yaml", ksailYAML)
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n",
	)
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	setupMockRegistryBackend(t)

	cmd := cluster.NewCreateCmd(newTestRuntimeContainer(t))

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

func TestShouldPushOCIArtifact_FluxWithLocalRegistry(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.True(t, result, "Should push when Flux is enabled with local registry")
}

func TestShouldPushOCIArtifact_ArgoCDWithLocalRegistry(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.True(t, result, "Should push when ArgoCD is enabled with local registry")
}

func TestShouldPushOCIArtifact_NoLocalRegistryShouldNotPush(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					// Empty registry - disabled
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.False(t, result, "Should not push when local registry is disabled")
}

func TestShouldPushOCIArtifact_NoGitOpsEngineShouldNotPush(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineNone,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.False(t, result, "Should not push when GitOps engine is none")
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.Provisioner = (*fakeProvisioner)(nil)
	_ clusterprovisioner.Factory     = (*fakeFactory)(nil)
	_ installer.Installer            = (*fakeInstaller)(nil)
)

func TestSetupK3dCSI_DisablesCSI(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Verify the flag was added
	found := false

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=local-storage" {
			found = true

			require.Equal(t, []string{"server:*"}, arg.NodeFilters)

			break
		}
	}

	require.True(t, found, "--disable=local-storage flag should be added")
}

func TestSetupK3dCSI_DoesNotDuplicateFlag(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{
		Options: v1alpha5.SimpleConfigOptions{
			K3sOptions: v1alpha5.SimpleConfigOptionsK3s{
				ExtraArgs: []v1alpha5.K3sArgWithNodeFilters{
					{
						Arg:         "--disable=local-storage",
						NodeFilters: []string{"server:*"},
					},
				},
			},
		},
	}

	cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Count occurrences of the flag
	count := 0

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=local-storage" {
			count++
		}
	}

	require.Equal(t, 1, count, "flag should not be duplicated")
}

func TestSetupK3dCSI_DoesNothingForNonK3s(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Verify no flags were added
	require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
}

func TestSetupK3dCSI_DoesNothingWhenCSINotDisabled(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		csi  v1alpha1.CSI
	}{
		{"default", v1alpha1.CSIDefault},
		{"enabled", v1alpha1.CSIEnabled},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						CSI:          testCase.csi,
					},
				},
			}

			k3dConfig := &v1alpha5.SimpleConfig{}

			cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

			// Verify no flags were added
			require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
		})
	}
}

func TestResolveClusterNameFromContext_Vanilla(t *testing.T) {
	t.Parallel()

	kindConfig := &v1alpha4.Cluster{Name: "kind-cluster"}
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
				},
			},
		},
		KindConfig: kindConfig,
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "kind-cluster", name)
}

func TestResolveClusterNameFromContext_K3s(t *testing.T) {
	t.Parallel()

	k3dConfig := &v1alpha5.SimpleConfig{
		ObjectMeta: types.ObjectMeta{
			Name: "k3s-cluster",
		},
	}
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionK3s,
				},
			},
		},
		K3dConfig: k3dConfig,
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "k3s-cluster", name)
}

func TestResolveClusterNameFromContext_FallbackToContext(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.Distribution("unknown"),
					Connection: v1alpha1.Connection{
						Context: "custom-context",
					},
				},
			},
		},
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "custom-context", name)
}

func TestResolveClusterNameFromContext_FallbackToDefault(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.Distribution("unknown"),
				},
			},
		},
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "ksail", name)
}

//nolint:dupl // structural similarity with restore_test.go is a false positive — different function under test
func TestMatchesKindPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "exact control-plane match",
			containerName: "myapp-control-plane",
			clusterName:   "myapp",
			want:          true,
		},
		{
			name:          "exact worker match",
			containerName: "myapp-worker",
			clusterName:   "myapp",
			want:          true,
		},
		{
			name:          "numbered worker",
			containerName: "myapp-worker3",
			clusterName:   "myapp",
			want:          true,
		},
		{
			name:          "control-plane prefix mismatch",
			containerName: "myapp2-control-plane",
			clusterName:   "myapp",
			want:          false,
		},
		{
			name:          "control-plane for different cluster",
			containerName: "other-control-plane",
			clusterName:   "myapp",
			want:          false,
		},
		{
			name:          "K3d container not a Kind pattern",
			containerName: "k3d-myapp-server-0",
			clusterName:   "myapp",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportMatchesKindPattern(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsNumericString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "single digit", input: "0", want: true},
		{name: "multi digit", input: "123", want: true},
		{name: "large number", input: "9999", want: true},
		{name: "empty string", input: "", want: false},
		{name: "alpha string", input: "abc", want: false},
		{name: "alphanumeric", input: "1a2b", want: false},
		{name: "negative sign", input: "-1", want: false},
		{name: "whitespace", input: " 1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportIsNumericString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsCloudProviderKindContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		want          bool
	}{
		{
			name:          "exact ksail-cloud-provider-kind name",
			containerName: "ksail-cloud-provider-kind",
			want:          true,
		},
		{
			name:          "cpk-prefixed service container",
			containerName: "cpk-lb",
			want:          true,
		},
		{
			name:          "another cpk-prefixed container",
			containerName: "cpk-worker-1",
			want:          true,
		},
		{
			name:          "kind control-plane is not cloud-provider-kind",
			containerName: "dev-control-plane",
			want:          false,
		},
		{
			name:          "k3d server is not cloud-provider-kind",
			containerName: "k3d-dev-server-0",
			want:          false,
		},
		{
			name:          "empty string",
			containerName: "",
			want:          false,
		},
		{
			name:          "cloud-provider-kind substring in name",
			containerName: "old-ksail-cloud-provider-kind",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportIsCloudProviderKindContainer(tt.containerName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsKindClusterFromNodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		nodes       []string
		clusterName string
		want        bool
	}{
		{
			name:        "single control-plane node identifies Kind cluster",
			nodes:       []string{"dev-control-plane"},
			clusterName: "dev",
			want:        true,
		},
		{
			name:        "worker node also identifies Kind cluster",
			nodes:       []string{"dev-worker"},
			clusterName: "dev",
			want:        true,
		},
		{
			name:        "control-plane plus workers",
			nodes:       []string{"dev-control-plane", "dev-worker", "dev-worker2"},
			clusterName: "dev",
			want:        true,
		},
		{
			name:        "k3d nodes do not match Kind pattern",
			nodes:       []string{"k3d-dev-server-0", "k3d-dev-agent-0"},
			clusterName: "dev",
			want:        false,
		},
		{
			name:        "talos nodes do not match Kind pattern",
			nodes:       []string{"dev-controlplane-0", "dev-worker-0"},
			clusterName: "dev",
			want:        false,
		},
		{
			name:        "empty node list",
			nodes:       []string{},
			clusterName: "dev",
			want:        false,
		},
		{
			name:        "nil node list",
			nodes:       nil,
			clusterName: "dev",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportIsKindClusterFromNodes(tt.nodes, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyDistributionSpecOverrides(t *testing.T) { //nolint:funlen
	t.Parallel()

	tests := []struct {
		name  string
		input v1alpha1.ClusterSpec
		want  v1alpha1.ClusterSpec
	}{
		{
			name: "KWOK with Flux and Kyverno and LoadBalancer Enabled normalises all three",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				PolicyEngine: v1alpha1.PolicyEngineKyverno,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineNone,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
				CNI:          v1alpha1.CNIDefault,
				CSI:          v1alpha1.CSIDisabled,
				CertManager:  v1alpha1.CertManagerDisabled,
			},
		},
		{
			name: "KWOK with ArgoCD keeps GitOpsEngine unchanged",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
				CNI:          v1alpha1.CNIDefault,
				CSI:          v1alpha1.CSIDisabled,
				CertManager:  v1alpha1.CertManagerDisabled,
			},
		},
		{
			name: "KWOK with Calico normalises CNI to Default",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				CNI:          v1alpha1.CNICalico,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionKWOK,
				PolicyEngine: v1alpha1.PolicyEngineNone,
				LoadBalancer: v1alpha1.LoadBalancerDisabled,
				CNI:          v1alpha1.CNIDefault,
				CSI:          v1alpha1.CSIDisabled,
				CertManager:  v1alpha1.CertManagerDisabled,
			},
		},
		{
			name: "non-KWOK distribution is unchanged",
			input: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				PolicyEngine: v1alpha1.PolicyEngineKyverno,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
			want: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				PolicyEngine: v1alpha1.PolicyEngineKyverno,
				LoadBalancer: v1alpha1.LoadBalancerEnabled,
			},
		},
	}

	for _, tt := range tests { //nolint:varnamelen // tt is conventional for table-driven tests
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec := tt.input
			cluster.ExportApplyDistributionSpecOverrides(&spec)
			assert.Equal(t, tt.want, spec)
		})
	}
}

// fakeDeleteProvisioner is a fake provisioner for delete tests.
type fakeDeleteProvisioner struct {
	existsResult bool
	deleteErr    error
}

func (*fakeDeleteProvisioner) Create(context.Context, string) error { return nil }
func (f *fakeDeleteProvisioner) Delete(context.Context, string) error {
	return f.deleteErr
}
func (*fakeDeleteProvisioner) Start(context.Context, string) error { return nil }
func (*fakeDeleteProvisioner) Stop(context.Context, string) error  { return nil }
func (*fakeDeleteProvisioner) List(context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeDeleteProvisioner) Exists(context.Context, string) (bool, error) {
	return f.existsResult, nil
}

// fakeDeleteFactory creates a provisioner for delete tests.
type fakeDeleteFactory struct {
	existsResult bool
	deleteErr    error
}

func (f fakeDeleteFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeDeleteProvisioner{
		existsResult: f.existsResult,
		deleteErr:    f.deleteErr,
	}, cfg, nil
}

// writeKubeconfigWithContext creates a kubeconfig file with the given current context.
func writeKubeconfigWithContext(t *testing.T, dir, currentContext string) string {
	t.Helper()

	kubeconfigContent := `apiVersion: v1
kind: Config
current-context: ` + currentContext + `
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: ` + currentContext + `
contexts:
- context:
    cluster: ` + currentContext + `
    user: ` + currentContext + `
  name: ` + currentContext + `
users:
- name: ` + currentContext + `
  user: {}
`
	kubeconfigPath := filepath.Join(dir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfigContent), 0o600))

	return kubeconfigPath
}

func newDeleteTestRuntimeContainer(t *testing.T) *di.Runtime {
	t.Helper()

	return di.New(
		func(i di.Injector) error {
			do.Provide(i, func(di.Injector) (timer.Timer, error) {
				return timer.New(), nil
			})

			return nil
		},
	)
}

// trimTrailingNewlineDelete removes a single trailing newline from snapshot output.
func trimTrailingNewlineDelete(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}

	return s
}

// setupContextBasedTest sets up a test environment for context-based detection tests.
// Returns a cleanup function that must be called with defer.
func setupContextBasedTest(
	t *testing.T,
	contextName string,
	existsResult bool,
	deleteErr error,
) (*di.Runtime, func()) {
	t.Helper()

	workingDir := t.TempDir()
	t.Chdir(workingDir)

	kubeconfigPath := writeKubeconfigWithContext(t, workingDir, contextName)
	t.Setenv("KUBECONFIG", kubeconfigPath)

	restoreFactory := cluster.SetProvisionerFactoryForTests(
		fakeDeleteFactory{existsResult: existsResult, deleteErr: deleteErr},
	)

	// Override Docker client to skip cleanup (no Docker in tests)
	restoreDocker := cluster.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, _ func(client.APIClient) error) error {
			return nil // Skip Docker operations in tests
		},
	)

	// Override TTY check to return false by default (non-interactive mode)
	// This ensures existing tests don't prompt for confirmation
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return false })

	testRuntime := newDeleteTestRuntimeContainer(t)

	cleanup := func() {
		restoreTTY()
		restoreDocker()
		restoreFactory()
	}

	return testRuntime, cleanup
}

// TestDelete_ContextBasedDetection_DeletesCluster tests that delete can detect
// cluster from kubeconfig context and delete the cluster successfully.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_ContextBasedDetection_DeletesCluster(t *testing.T) {
	testCases := []struct {
		name    string
		context string
	}{
		{name: "Kind_context_pattern", context: "kind-my-cluster"},
		{name: "K3d_context_pattern", context: "k3d-dev-cluster"},
		{name: "Talos_context_pattern", context: "admin@talos-homelab"},
	}

	for _, testCase := range testCases {
		//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
		t.Run(testCase.name, func(t *testing.T) {
			testRuntime, cleanup := setupContextBasedTest(t, testCase.context, true, nil)
			defer cleanup()

			cmd := cluster.NewDeleteCmd(testRuntime)

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())

			err := cmd.Execute()
			require.NoError(t, err)

			output := out.String()
			require.Contains(t, output, "cluster deleted")

			snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
		})
	}
}

// TestDelete_ContextBasedDetection_ClusterNotFound tests that context-based detection
// correctly returns an error when the detected cluster doesn't exist.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_ContextBasedDetection_ClusterNotFound(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(
		t,
		"kind-nonexistent",
		false,
		clustererr.ErrClusterNotFound,
	)
	defer cleanup()

	cmd := cluster.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, clustererr.ErrClusterNotFound)

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(out.String()))
}

// TestDelete_ContextBasedDetection_UnknownContextPattern tests that delete returns
// an error when the context doesn't match a known pattern.
func TestDelete_ContextBasedDetection_UnknownContextPattern(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	kubeconfigPath := writeKubeconfigWithContext(t, workingDir, "docker-desktop")
	t.Setenv("KUBECONFIG", kubeconfigPath)

	// Override Docker client to skip cleanup (no Docker in tests)
	restoreDocker := cluster.SetDockerClientInvokerForTests(
		func(_ *cobra.Command, _ func(client.APIClient) error) error {
			return nil // Skip Docker operations in tests
		},
	)
	defer restoreDocker()

	testRuntime := newDeleteTestRuntimeContainer(t)

	cmd := cluster.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	// Error should indicate cluster name is required
	require.Contains(t, err.Error(), "cluster name is required")
}

// TestDelete_CommandFlags verifies that the delete command has the expected flags.
func TestDelete_CommandFlags(t *testing.T) {
	t.Parallel()

	testRuntime := newDeleteTestRuntimeContainer(t)
	cmd := cluster.NewDeleteCmd(testRuntime)

	// Verify expected new flags exist
	nameFlag := cmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag, "expected --name flag")
	require.Equal(t, "n", nameFlag.Shorthand)

	providerFlag := cmd.Flags().Lookup("provider")
	require.NotNil(t, providerFlag, "expected --provider flag")
	require.Equal(t, "p", providerFlag.Shorthand)

	kubeconfigFlag := cmd.Flags().Lookup("kubeconfig")
	require.NotNil(t, kubeconfigFlag, "expected --kubeconfig flag")
	require.Equal(t, "k", kubeconfigFlag.Shorthand)

	deleteStorageFlag := cmd.Flags().Lookup("delete-storage")
	require.NotNil(t, deleteStorageFlag, "expected --delete-storage flag")

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag, "expected --force flag")
	require.Equal(t, "f", forceFlag.Shorthand)

	// Verify old flags do NOT exist
	contextFlag := cmd.Flags().Lookup("context")
	require.Nil(t, contextFlag, "unexpected --context flag (should be removed)")

	distributionFlag := cmd.Flags().Lookup("distribution")
	require.Nil(t, distributionFlag, "unexpected --distribution flag (should be removed)")
}

func TestConnect_CommandFlags(t *testing.T) {
	t.Parallel()

	cmd := cluster.NewConnectCmd(nil)

	// Verify expected flags exist
	contextFlag := cmd.Flags().Lookup("context")
	require.NotNil(t, contextFlag, "expected --context flag")
	require.Equal(t, "c", contextFlag.Shorthand)

	kubeconfigFlag := cmd.Flags().Lookup("kubeconfig")
	require.NotNil(t, kubeconfigFlag, "expected --kubeconfig flag")
	require.Equal(t, "k", kubeconfigFlag.Shorthand)

	editorFlag := cmd.Flags().Lookup("editor")
	require.NotNil(t, editorFlag, "expected --editor flag")

	// Verify hidden flags exist but are hidden (needed for config defaults/validation)
	distributionFlag := cmd.Flags().Lookup("distribution")
	require.NotNil(t, distributionFlag, "expected --distribution flag (hidden)")
	require.True(t, distributionFlag.Hidden, "--distribution should be hidden")

	distributionConfigFlag := cmd.Flags().Lookup("distribution-config")
	require.NotNil(t, distributionConfigFlag, "expected --distribution-config flag (hidden)")
	require.True(t, distributionConfigFlag.Hidden, "--distribution-config should be hidden")

	gitopsEngineFlag := cmd.Flags().Lookup("gitops-engine")
	require.NotNil(t, gitopsEngineFlag, "expected --gitops-engine flag (hidden)")
	require.True(t, gitopsEngineFlag.Hidden, "--gitops-engine should be hidden")

	localRegistryFlag := cmd.Flags().Lookup("local-registry")
	require.NotNil(t, localRegistryFlag, "expected --local-registry flag (hidden)")
	require.True(t, localRegistryFlag.Hidden, "--local-registry should be hidden")
}

// TestDelete_Confirmation_Accepted tests that deletion proceeds when user confirms with "yes".
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_Confirmation_Accepted(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock stdin to return "yes"
	stdinReader := strings.NewReader("yes\n")

	restoreStdin := confirm.SetStdinReaderForTests(stdinReader)
	defer restoreStdin()

	// Mock TTY check to return true (simulates interactive terminal)
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restoreTTY()

	cmd := cluster.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	require.Contains(t, output, "cluster deleted")
	require.Contains(t, output, "The following resources will be deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// TestDelete_Confirmation_Denied tests that deletion is cancelled when user does not confirm.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_Confirmation_Denied(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock stdin to return "no"
	stdinReader := strings.NewReader("no\n")

	restoreStdin := confirm.SetStdinReaderForTests(stdinReader)
	defer restoreStdin()

	// Mock TTY check to return true (simulates interactive terminal)
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restoreTTY()

	cmd := cluster.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, confirm.ErrDeletionCancelled)

	output := out.String()
	require.Contains(t, output, "The following resources will be deleted")
	require.NotContains(t, output, "cluster deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// TestDelete_ForceFlag_SkipsConfirmation tests that --force flag bypasses the confirmation prompt.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_ForceFlag_SkipsConfirmation(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock TTY check to return true (simulates interactive terminal)
	// Note: stdin is NOT mocked - if prompt runs, it will fail to read
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return true })
	defer restoreTTY()

	cmd := cluster.NewDeleteCmd(testRuntime)
	cmd.SetArgs([]string{"--force"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	require.Contains(t, output, "cluster deleted")
	// Should NOT show confirmation preview when --force is used
	require.NotContains(t, output, "The following resources will be deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// TestDelete_NonTTY_SkipsConfirmation tests that non-TTY environments skip the confirmation prompt.
//
//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir() and t.Setenv() in helper
func TestDelete_NonTTY_SkipsConfirmation(t *testing.T) {
	testRuntime, cleanup := setupContextBasedTest(t, "kind-my-cluster", true, nil)
	defer cleanup()

	// Mock TTY check to return false (simulates CI/pipeline environment)
	restoreTTY := confirm.SetTTYCheckerForTests(func() bool { return false })
	defer restoreTTY()

	cmd := cluster.NewDeleteCmd(testRuntime)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.NoError(t, err)

	output := out.String()
	require.Contains(t, output, "cluster deleted")
	// Should NOT show confirmation preview when stdin is not a TTY
	require.NotContains(t, output, "The following resources will be deleted")

	snaps.MatchSnapshot(t, trimTrailingNewlineDelete(output))
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.Provisioner = (*fakeDeleteProvisioner)(nil)
	_ clusterprovisioner.Factory     = (*fakeDeleteFactory)(nil)
)

// containerTestCase is a test case for IsClusterContainer.
type containerTestCase struct {
	name          string
	containerName string
	clusterName   string
	expected      bool
}

// getContainerTestCases returns test cases for IsClusterContainer.
func getContainerTestCases() []containerTestCase {
	return []containerTestCase{
		// Kind patterns
		{"kind_control_plane", "my-cluster-control-plane", "my-cluster", true},
		{"kind_control_plane_staging", "staging-control-plane", "staging", true},
		{"kind_control_plane_prefix_clash", "dev2-control-plane", "dev", false},
		{"kind_control_plane_suffix_only", "-control-plane", "dev", false},
		{"kind_worker", "my-cluster-worker", "my-cluster", true},
		{"kind_worker_numbered", "my-cluster-worker2", "my-cluster", true},
		{"kind_worker_10", "dev-worker10", "dev", true},
		{"kind_worker_non_numeric_suffix", "dev-workerabc", "dev", false},
		{"kind_worker_prefix_clash", "devprod-worker", "dev", false},
		{"kind_worker_alphanumeric_suffix", "dev-worker2a", "dev", false},

		// K3d patterns
		{"k3d_server", "k3d-my-cluster-server-0", "my-cluster", true},
		{"k3d_agent", "k3d-my-cluster-agent-0", "my-cluster", true},
		{"k3d_server_1", "k3d-dev-server-1", "dev", true},
		{"k3d_agent_multi_digit", "k3d-dev-agent-10", "dev", true},
		{"k3d_different_cluster", "k3d-staging-server-0", "dev", false},
		{"k3d_prefix_clash", "k3d-dev2-server-0", "dev", false},
		{"k3d_missing_role", "k3d-dev-0", "dev", false},

		// Talos patterns
		{"talos_controlplane", "my-cluster-controlplane-1", "my-cluster", true},
		{"talos_worker", "my-cluster-worker-1", "my-cluster", true},
		{"talos_worker_0", "dev-worker-0", "dev", true},
		{"talos_different_cluster", "staging-controlplane-0", "dev", false},
		{"talos_prefix_clash", "dev2-controlplane-0", "dev", false},

		// VCluster patterns
		{"vcluster_cp", "vcluster.cp.my-cluster", "my-cluster", true},
		{"vcluster_cp_different_cluster", "vcluster.cp.other-cluster", "my-cluster", false},
		{"vcluster_prefix_partial_match", "vcluster.cp.dev-extra", "dev", false},

		// Non-matching
		{"different_cluster", "other-cluster-control-plane", "my-cluster", false},
		{"registry_container", "registry.localhost", "my-cluster", false},
		{"partial_match", "my-cluster-registry", "my-cluster", false},
		{"prefix_collision", "my-cluster-test-control-plane", "my-cluster", false},
		{"unrelated_container", "nginx", "dev", false},
		{"empty_container_name", "", "dev", false},
		{"empty_cluster_name", "dev-control-plane", "", false},
		{"cloud_provider_kind", "ksail-cloud-provider-kind", "dev", false},
		{"cpk_service", "cpk-lb", "dev", false},
	}
}

// TestIsClusterContainer tests the container name matching logic.
func TestIsClusterContainer(t *testing.T) {
	t.Parallel()

	for _, testCase := range getContainerTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.IsClusterContainer(testCase.containerName, testCase.clusterName)
			require.Equal(t, testCase.expected, result)
		})
	}
}

const mirrorRegistryHelp = "Configure mirror registries with format 'host=upstream' " +
	"(e.g., docker.io=https://registry-1.docker.io)."

func setFlags(t *testing.T, cmd *cobra.Command, values map[string]string) {
	t.Helper()

	for k, v := range values {
		err := cmd.Flags().Set(k, v)
		if err != nil {
			t.Fatalf("failed to set flag %s: %v", k, err)
		}
	}
}

func newInitCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "init"}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	return cmd
}

func newConfigManager(
	t *testing.T,
	cmd *cobra.Command,
	writer io.Writer,
) *ksailconfigmanager.ConfigManager {
	t.Helper()
	cmd.SetOut(writer)
	cmd.SetErr(writer)
	manager := ksailconfigmanager.NewCommandConfigManager(cmd, cluster.InitFieldSelectors())
	// bind init-local flags like production code
	cmd.Flags().StringP("output", "o", "", "Output directory for the project")
	_ = manager.Viper.BindPFlag("output", cmd.Flags().Lookup("output"))
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing files")
	_ = manager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))
	cmd.Flags().
		StringSlice("mirror-registry", []string{}, mirrorRegistryHelp)
	// NOTE: mirror-registry is NOT bound to Viper to allow custom merge logic in production
	// Tests that need to check mirror values should call getMirrorRegistriesWithDefaults()

	return manager
}

// writeKsailConfig creates a ksail.yaml config file in the specified directory.
func writeKsailConfig(t *testing.T, outDir string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "ksail.yaml"), []byte(content), 0o600))
}

// setupInitTest sets up a test command with configuration manager and common flags.
func setupInitTest(
	t *testing.T,
	outDir string,
	force bool,
	buffer *bytes.Buffer,
) (*cobra.Command, *ksailconfigmanager.ConfigManager) {
	t.Helper()
	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, buffer)

	forceStr := strconv.FormatBool(force)

	setFlags(t, cmd, map[string]string{
		"output": outDir,
		"force":  forceStr,
	})

	return cmd, cfgManager
}

func TestHandleInitRunE_SuccessWithOutputFlag(t *testing.T) {
	t.Parallel()

	// Using mockery-generated Timer (pkg/ui/timer/mocks.go) so we can set deterministic
	// expectations on timing calls without maintaining a bespoke RecordingTimer helper.

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd, cfgManager := setupInitTest(t, outDir, true, &buffer)

	deps := newInitDeps(t)

	var err error

	err = cluster.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	// Expectations asserted via mock cleanup

	snaps.MatchSnapshot(t, buffer.String())

	_, err = os.Stat(filepath.Join(outDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}
}

func TestHandleInitRunE_RespectsDistributionFlag(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, &buffer)

	setFlags(t, cmd, map[string]string{
		"output":              outDir,
		"distribution":        "K3s",
		"distribution-config": "k3d.yaml",
		"force":               "true",
	})

	deps := newInitDeps(t)

	err := cluster.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	_, err = os.Stat(filepath.Join(outDir, "k3d.yaml"))
	if err != nil {
		t.Fatalf("expected k3d.yaml to be scaffolded: %v", err)
	}
}

//nolint:funlen // Test function includes comprehensive assertions for Talos scaffolding
func TestHandleInitRunE_RespectsDistributionFlagTalos(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	var buffer bytes.Buffer

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, &buffer)

	setFlags(t, cmd, map[string]string{
		"output":       outDir,
		"distribution": "Talos",
		"force":        "true",
	})

	deps := newInitDeps(t)

	err := cluster.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	// Verify the talos patches directory structure was created
	// Note: .gitkeep is NOT created in cluster/ because allow-scheduling patch is generated there
	expectedPaths := []string{
		filepath.Join(outDir, "talos", "control-planes", ".gitkeep"),
		filepath.Join(outDir, "talos", "workers", ".gitkeep"),
		filepath.Join(outDir, "talos", "cluster", "allow-scheduling-on-control-planes.yaml"),
	}

	for _, path := range expectedPaths {
		_, err = os.Stat(path)
		if err != nil {
			t.Fatalf("expected path to be scaffolded: %s, error: %v", path, err)
		}
	}

	// Verify allow-scheduling-on-control-planes.yaml content
	allowSchedulingPath := filepath.Join(
		outDir,
		"talos",
		"cluster",
		"allow-scheduling-on-control-planes.yaml",
	)

	//nolint:gosec // Test file path is safe
	allowSchedulingContent, err := os.ReadFile(allowSchedulingPath)
	if err != nil {
		t.Fatalf("expected allow-scheduling-on-control-planes.yaml to be scaffolded: %v", err)
	}

	if !strings.Contains(string(allowSchedulingContent), "allowSchedulingOnControlPlanes: true") {
		t.Fatalf(
			"expected allow-scheduling-on-control-planes.yaml to contain correct config\n%s",
			allowSchedulingContent,
		)
	}

	// Verify ksail.yaml contains Talos distribution
	ksailPath := filepath.Join(outDir, "ksail.yaml")

	content, err := os.ReadFile(ksailPath) //nolint:gosec // Test file path is safe
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}

	if !strings.Contains(string(content), "distribution: Talos") {
		t.Fatalf("expected ksail.yaml to contain Talos distribution\n%s", content)
	}

	// Verify output contains created files
	// Note: cluster/.gitkeep is NOT in output because allow-scheduling patch replaces it
	output := buffer.String()
	if !strings.Contains(output, "talos/control-planes/.gitkeep") {
		t.Fatalf("expected output to mention created talos directory structure\n%s", output)
	}
}

//nolint:paralleltest // Uses t.Chdir for snapshot setup.
func TestHandleInitRunE_UsesWorkingDirectoryWhenOutputUnset(t *testing.T) {
	workingDir := t.TempDir()

	var buffer bytes.Buffer

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, &buffer)

	t.Chdir(workingDir)

	setFlags(t, cmd, map[string]string{
		"force": "true",
	})

	deps := newInitDeps(t)

	var err error

	err = cluster.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	snaps.MatchSnapshot(t, buffer.String())

	_, err = os.Stat(filepath.Join(workingDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml in working directory: %v", err)
	}
}

func TestHandleInitRunE_DefaultsLocalRegistryWithFlux(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, io.Discard)

	setFlags(t, cmd, map[string]string{
		"output":        outDir,
		"force":         "true",
		"gitops-engine": "Flux",
	})

	deps := newInitDeps(t)

	err := cluster.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	//nolint:gosec // test file path is safe
	content, err := os.ReadFile(filepath.Join(outDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}

	// With the new single-field design, local registry is enabled when the registry field is populated
	if !strings.Contains(string(content), "localRegistry:") ||
		!strings.Contains(string(content), "registry: localhost:5050") {
		t.Fatalf("expected ksail.yaml to enable local registry when Flux is selected\n%s", content)
	}
}

func TestHandleInitRunE_RespectsCertManagerFlag(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()

	cmd := newInitCommand(t)
	cfgManager := newConfigManager(t, cmd, io.Discard)

	setFlags(t, cmd, map[string]string{
		"output":       outDir,
		"force":        "true",
		"cert-manager": "Enabled",
	})

	deps := newInitDeps(t)

	err := cluster.HandleInitRunE(cmd, cfgManager, deps)
	if err != nil {
		t.Fatalf("HandleInitRunE returned error: %v", err)
	}

	//nolint:gosec // test file path is safe
	content, err := os.ReadFile(filepath.Join(outDir, "ksail.yaml"))
	if err != nil {
		t.Fatalf("expected ksail.yaml to be scaffolded: %v", err)
	}

	if !strings.Contains(string(content), "certManager: Enabled") {
		t.Fatalf("expected ksail.yaml to enable cert-manager when flag is set\n%s", content)
	}
}

func TestHandleInitRunE_IgnoresExistingConfigFile(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()
	existing := "apiVersion: ksail.io/v1alpha1\n" +
		"kind: Cluster\n" +
		"spec:\n" +
		"  distribution: K3s\n" +
		"  distributionConfig: custom-k3d.yaml\n" +
		"  sourceDirectory: legacy\n"

	writeKsailConfig(t, outDir, existing)

	var buffer bytes.Buffer

	cmd, cfgManager := setupInitTest(t, outDir, true, &buffer)

	deps := newInitDeps(t)

	err := cluster.HandleInitRunE(cmd, cfgManager, deps)
	require.NoError(t, err)

	//nolint:gosec // test file path is safe
	content, readErr := os.ReadFile(filepath.Join(outDir, "ksail.yaml"))
	require.NoError(t, readErr)

	// Ensure defaults are applied instead of values from the existing file.
	if strings.Contains(string(content), "distribution: K3s") {
		t.Fatalf("unexpected prior distribution carried over\n%s", string(content))
	}

	if strings.Contains(string(content), "distributionConfig: custom-k3d.yaml") {
		t.Fatalf("unexpected prior distributionConfig carried over\n%s", string(content))
	}

	if strings.Contains(string(content), "sourceDirectory: legacy") {
		t.Fatalf("unexpected prior sourceDirectory carried over\n%s", string(content))
	}
}

func newInitDeps(t *testing.T) cluster.InitDeps {
	t.Helper()
	tmr := timer.NewMockTimer(t)
	tmr.EXPECT().Start().Return()
	tmr.EXPECT().NewStage().Return()

	return cluster.InitDeps{Timer: tmr}
}

var (
	errTestListClusters = errors.New("failed to list clusters")
	errTestFactoryError = errors.New("factory error")
)

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

// fakeProvisionerWithClusters returns a list of clusters for testing.
type fakeProvisionerWithClusters struct {
	clusters []string
	listErr  error
}

func (f *fakeProvisionerWithClusters) Create(context.Context, string) error { return nil }
func (f *fakeProvisionerWithClusters) Delete(context.Context, string) error { return nil }
func (f *fakeProvisionerWithClusters) Start(context.Context, string) error  { return nil }
func (f *fakeProvisionerWithClusters) Stop(context.Context, string) error   { return nil }
func (f *fakeProvisionerWithClusters) List(context.Context) ([]string, error) {
	return f.clusters, f.listErr
}

func (f *fakeProvisionerWithClusters) Exists(context.Context, string) (bool, error) {
	return len(f.clusters) > 0, nil
}

// fakeFactoryWithClusters creates a provisioner that returns clusters.
type fakeFactoryWithClusters struct {
	clusters []string
	listErr  error
}

func (f fakeFactoryWithClusters) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	cfg := &v1alpha4.Cluster{Name: "test"}

	return &fakeProvisionerWithClusters{clusters: f.clusters, listErr: f.listErr}, cfg, nil
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_NoClusterFound_DockerProvider(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{}}
		},
	}

	// Filter to Docker provider - no output for empty list
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_SingleClusterFound_DockerProvider(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
	}

	// Filter to Docker provider
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_MultipleClustersFound_DockerProvider(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				clusters: []string{"cluster-1", "cluster-2", "cluster-3"},
			}
		},
	}

	// Filter to Docker provider
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

func TestListCmd_AllProviders(t *testing.T) {
	// Clear HCLOUD_TOKEN to ensure Hetzner provider is skipped
	t.Setenv("HCLOUD_TOKEN", "")

	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	// Create a mock factory that returns test-cluster for all distributions
	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test-cluster"}}
		},
	}

	// No filter = list all providers (default behavior)
	// Hetzner will be skipped since HCLOUD_TOKEN is cleared
	err := cluster.HandleListRunE(cmd, "", deps)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buf.String())
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_ListError(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{
				listErr: fmt.Errorf("test error: %w", errTestListClusters),
			}
		},
	}

	// List errors per distribution are silently skipped - command succeeds with no clusters found
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	// Since all distributions fail, no clusters found
	require.Contains(t, outBuf.String(), "No clusters found")
}

//nolint:paralleltest // uses t.Chdir
func TestHandleListRunE_Success(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithClusters{clusters: []string{"test"}}
		},
	}

	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_FactoryError(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var outBuf, errBuf bytes.Buffer

	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{
		DistributionFactoryCreator: func(_ v1alpha1.Distribution) clusterprovisioner.Factory {
			return fakeFactoryWithErrors{}
		},
	}

	// Factory errors per distribution are silently skipped - command succeeds with no clusters found
	err := cluster.HandleListRunE(cmd, v1alpha1.ProviderDocker, deps)
	require.NoError(t, err)

	// Since all distributions fail, no clusters found
	require.Contains(t, outBuf.String(), "No clusters found")
}

//nolint:paralleltest // uses t.Chdir
func TestListCmd_InvalidProviderFilter(t *testing.T) {
	cmd := &cobra.Command{Use: "list"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	deps := cluster.ListDeps{}

	// Invalid provider filter is logged as warning, command still succeeds
	err := cluster.HandleListRunE(cmd, "InvalidProvider", deps)
	require.NoError(t, err)

	// Output should show no clusters found since invalid provider returns nothing
	require.Contains(t, buf.String(), "No clusters found")
}

// fakeFactoryWithErrors creates a provisioner that returns an error.
type fakeFactoryWithErrors struct{}

func (fakeFactoryWithErrors) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.Provisioner, any, error) {
	return nil, nil, fmt.Errorf("test error: %w", errTestFactoryError)
}

// Ensure fake types satisfy interfaces at compile time.
var (
	_ clusterprovisioner.Provisioner = (*fakeProvisionerWithClusters)(nil)
	_ clusterprovisioner.Factory     = (*fakeFactoryWithClusters)(nil)
	_ clusterprovisioner.Factory     = (*fakeFactoryWithErrors)(nil)
)

// newReconcileTestCmd creates a minimal cobra command for reconcile tests.
func newReconcileTestCmd() *cobra.Command {
	return &cobra.Command{Use: "test"}
}

// newReconcileTestClusterCfg creates a minimal cluster config for reconcile tests.
func newReconcileTestClusterCfg() *v1alpha1.Cluster {
	return &v1alpha1.Cluster{}
}

// TestHandlerForField_KnownFields verifies that each recognised component field has a handler.
func TestHandlerForField_KnownFields(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()

	knownFields := []string{
		"cluster.cni",
		"cluster.csi",
		"cluster.metricsServer",
		"cluster.loadBalancer",
		"cluster.certManager",
		"cluster.policyEngine",
		"cluster.gitOpsEngine",
	}

	for _, field := range knownFields {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			found := cluster.ExportHandlerForField(cmd, clusterCfg, field)

			assert.True(t, found, "expected a handler to be registered for field %q", field)
		})
	}
}

// TestHandlerForField_UnknownField verifies that unrecognised fields have no handler.
func TestHandlerForField_UnknownField(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()

	fields := []string{
		"cluster.unknown",
		"cluster.nodes",
		"cluster.mirrorRegistries",
		"",
	}

	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			found := cluster.ExportHandlerForField(cmd, clusterCfg, field)

			assert.False(t, found, "expected no handler for field %q", field)
		})
	}
}

// TestReconcileMetricsServer_DisabledReturnsError verifies that attempting to disable
// metrics-server in-place returns the unsupported-operation error.
func TestReconcileMetricsServer_DisabledReturnsError(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.metricsServer",
		OldValue: string(v1alpha1.MetricsServerEnabled),
		NewValue: string(v1alpha1.MetricsServerDisabled),
	}

	err := cluster.ExportReconcileMetricsServer(cmd, clusterCfg, change)

	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrMetricsServerDisableUnsupported)
}

// TestReconcileCSI_NilFactory verifies that reconcileCSI returns an error when the CSI
// installer factory has not been configured.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCSI_NilFactory(t *testing.T) {
	restore := cluster.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.csi",
		OldValue: string(v1alpha1.CSIDisabled),
		NewValue: "hetznercsi",
	}

	err := cluster.ExportReconcileCSI(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCSIInstallerFactoryNil)
}

// TestReconcileCSI_NilFactory_DisabledToDisabled documents that the nil-factory guard fires
// before the disabled-to-disabled no-op check, so a nil factory returns an error even for
// disabled→disabled transitions.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCSI_NilFactory_DisabledToDisabled(t *testing.T) {
	// nil CSI factory — the nil-factory guard fires before the disabled no-op check
	restore := cluster.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.csi",
		OldValue: string(v1alpha1.CSIDisabled),
		NewValue: string(v1alpha1.CSIDisabled),
	}

	// The nil-factory guard fires before the no-op check, so this returns an error.
	// Document this known behaviour to prevent regressions.
	err := cluster.ExportReconcileCSI(cmd, clusterCfg, change)
	require.Error(t, err, "nil factory is checked before the disabled no-op path")
	assert.ErrorIs(t, err, setup.ErrCSIInstallerFactoryNil)
}

// TestReconcileCertManager_DisabledFromDisabled_Noop verifies that disabling cert-manager
// when it was already disabled/empty is a no-op. A non-nil (but never-called) factory is
// required because the nil guard fires before the disabled no-op check.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCertManager_DisabledFromDisabled_Noop(t *testing.T) {
	// Factory must be non-nil to pass the nil guard; it will never actually be called.
	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			t.Fatal("factory should not be called for disabled→disabled transition") //nolint:revive
			panic("unreachable")
		},
	)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.certManager",
		OldValue: string(v1alpha1.CertManagerDisabled),
		NewValue: string(v1alpha1.CertManagerDisabled),
	}

	err := cluster.ExportReconcileCertManager(cmd, clusterCfg, change)

	require.NoError(t, err, "disabling when already disabled should be a no-op")
}

// TestReconcileCertManager_DisabledFromEnabled_NilFactory verifies that attempting to
// uninstall cert-manager with a nil factory returns the factory-nil error.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileCertManager_DisabledFromEnabled_NilFactory(t *testing.T) {
	restore := cluster.SetCertManagerInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.certManager",
		OldValue: string(v1alpha1.CertManagerEnabled),
		NewValue: string(v1alpha1.CertManagerDisabled),
	}

	err := cluster.ExportReconcileCertManager(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrCertManagerInstallerFactoryNil)
}

// TestReconcilePolicyEngine_NoneToNone_Noop verifies that transitioning from no policy
// engine to no policy engine is a no-op.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcilePolicyEngine_NoneToNone_Noop(t *testing.T) {
	restore := cluster.SetPolicyEngineInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.policyEngine",
		OldValue: string(v1alpha1.PolicyEngineNone),
		NewValue: string(v1alpha1.PolicyEngineNone),
	}

	err := cluster.ExportReconcilePolicyEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "None→None should be a no-op")
}

// TestReconcilePolicyEngine_NoneFromEnabled_NilFactory verifies that attempting to
// uninstall a policy engine with a nil factory returns the factory-nil error.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcilePolicyEngine_NoneFromEnabled_NilFactory(t *testing.T) {
	restore := cluster.SetPolicyEngineInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.policyEngine",
		OldValue: string(v1alpha1.PolicyEngineKyverno),
		NewValue: string(v1alpha1.PolicyEngineNone),
	}

	err := cluster.ExportReconcilePolicyEngine(cmd, clusterCfg, change)

	require.Error(t, err)
	assert.ErrorIs(t, err, setup.ErrPolicyEngineInstallerFactoryNil)
}

// TestReconcileGitOpsEngine_NoneToNone_Noop verifies that None→None is a no-op.
func TestReconcileGitOpsEngine_NoneToNone_Noop(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.gitOpsEngine",
		OldValue: string(v1alpha1.GitOpsEngineNone),
		NewValue: string(v1alpha1.GitOpsEngineNone),
	}

	err := cluster.ExportReconcileGitOpsEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "None→None should be a no-op")
}

// TestReconcileGitOpsEngine_EmptyToEmpty_Noop verifies that empty→empty is a no-op.
func TestReconcileGitOpsEngine_EmptyToEmpty_Noop(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	change := clusterupdate.Change{
		Field:    "cluster.gitOpsEngine",
		OldValue: "",
		NewValue: "",
	}

	err := cluster.ExportReconcileGitOpsEngine(cmd, clusterCfg, change)

	require.NoError(t, err, "empty→empty should be a no-op")
}

// TestReconcileComponents_EmptyDiff verifies that an empty diff results in no changes and no error.
func TestReconcileComponents_EmptyDiff(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err)
	assert.Empty(t, result.AppliedChanges)
	assert.Empty(t, result.FailedChanges)
}

// TestReconcileComponents_UnknownField_Skipped verifies that unknown field names are skipped.
func TestReconcileComponents_UnknownField_Skipped(t *testing.T) {
	t.Parallel()

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			{
				Field:    "cluster.nodes",
				OldValue: "1",
				NewValue: "3",
			},
		},
	}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err, "unknown fields should be skipped without error")
	assert.Empty(t, result.AppliedChanges, "unknown field should not be recorded as applied")
	assert.Empty(t, result.FailedChanges, "unknown field should not be recorded as failed")
}

// TestReconcileComponents_RecordsFailedChange verifies that a component error is captured
// in result.FailedChanges while processing of remaining changes continues.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileComponents_RecordsFailedChange(t *testing.T) {
	// Null CSI factory so the reconcileCSI call will fail immediately.
	restore := cluster.SetCSIInstallerFactoryForTests(nil)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			// This change will fail: nil CSI factory.
			{
				Field:    "cluster.csi",
				OldValue: string(v1alpha1.CSIDisabled),
				NewValue: "hetznercsi",
			},
			// This change will succeed: GitOps None→None is a no-op, no factory needed.
			{
				Field:    "cluster.gitOpsEngine",
				OldValue: string(v1alpha1.GitOpsEngineNone),
				NewValue: string(v1alpha1.GitOpsEngineNone),
			},
		},
	}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.Error(t, err, "expected error from nil CSI factory")
	require.Len(t, result.FailedChanges, 1)
	assert.Equal(t, "cluster.csi", result.FailedChanges[0].Field)
	// The GitOps no-op change is applied after the failure, confirming processing continues.
	require.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "cluster.gitOpsEngine", result.AppliedChanges[0].Field)
}

// TestReconcileComponents_MixedKnownAndUnknown verifies that known fields are processed
// and unknown fields are silently skipped in the same diff.
//
//nolint:paralleltest // mutates global installerFactoriesOverride; cannot run in parallel
func TestReconcileComponents_MixedKnownAndUnknown(t *testing.T) {
	// Provide a working cert-manager factory that returns an installer that succeeds.
	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			// Never called in this test because we test a disabled→disabled no-op.
			t.Fatal("factory should not be called for disabled→disabled transition") //nolint:revive
			panic("unreachable")
		},
	)
	t.Cleanup(restore)

	cmd := newReconcileTestCmd()
	clusterCfg := newReconcileTestClusterCfg()
	diff := &clusterupdate.UpdateResult{
		InPlaceChanges: []clusterupdate.Change{
			// Unknown field — should be skipped
			{Field: "cluster.nodes", OldValue: "1", NewValue: "3"},
			// Known field, disabled→disabled — should be a no-op (no error)
			{
				Field:    "cluster.certManager",
				OldValue: string(v1alpha1.CertManagerDisabled),
				NewValue: string(v1alpha1.CertManagerDisabled),
			},
			// Another unknown field — should be skipped
			{Field: "cluster.mirrorRegistries", OldValue: "a", NewValue: "b"},
		},
	}
	result := &clusterupdate.UpdateResult{}

	err := cluster.ExportReconcileComponents(cmd, clusterCfg, diff, result)

	require.NoError(t, err)
	// The certManager no-op is a handler returning nil — it is counted as applied.
	assert.Len(t, result.AppliedChanges, 1)
	assert.Equal(t, "cluster.certManager", result.AppliedChanges[0].Field)
	assert.Empty(t, result.FailedChanges)
}

// TestRestoreErrorConstants verifies that all sentinel error variables
// defined in restore.go are non-nil and have meaningful messages.
func TestRestoreErrorConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
		wantMsg  string
	}{
		{
			name:     "ErrInvalidResourcePolicy is defined",
			sentinel: cluster.ErrInvalidResourcePolicy,
			wantMsg:  "invalid existing-resource-policy",
		},
		{
			name:     "ErrRestoreFailed is defined",
			sentinel: cluster.ErrRestoreFailed,
			wantMsg:  "resource restore failed",
		},
		{
			name:     "ErrInvalidTarPath is defined",
			sentinel: cluster.ErrInvalidTarPath,
			wantMsg:  "invalid tar entry path",
		},
		{
			name:     "ErrSymlinkInArchive is defined",
			sentinel: cluster.ErrSymlinkInArchive,
			wantMsg:  "symbolic and hard links are not supported",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Error(t, testCase.sentinel)
			assert.Contains(t, testCase.sentinel.Error(), testCase.wantMsg)
		})
	}
}

// TestRestoreErrors_AreDistinct verifies that all restore error sentinels
// are distinct from one another so errors.Is behaves correctly.
func TestRestoreErrors_AreDistinct(t *testing.T) {
	t.Parallel()

	allErrors := []error{
		cluster.ErrInvalidResourcePolicy,
		cluster.ErrRestoreFailed,
		cluster.ErrInvalidTarPath,
		cluster.ErrSymlinkInArchive,
	}

	for index := range allErrors {
		for innerIndex := index + 1; innerIndex < len(allErrors); innerIndex++ {
			assert.NotErrorIs(
				t,
				allErrors[index], allErrors[innerIndex],
				"errors at index %d and %d should be distinct",
				index, innerIndex,
			)
		}
	}
}

// TestRestoreErrors_CanBeWrapped verifies that sentinel errors can be wrapped
// with fmt.Errorf and still be detected via errors.Is.
func TestRestoreErrors_CanBeWrapped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
	}{
		{
			name:     "ErrInvalidResourcePolicy can be wrapped",
			sentinel: cluster.ErrInvalidResourcePolicy,
		},
		{
			name:     "ErrRestoreFailed can be wrapped",
			sentinel: cluster.ErrRestoreFailed,
		},
		{
			name:     "ErrInvalidTarPath can be wrapped",
			sentinel: cluster.ErrInvalidTarPath,
		},
		{
			name:     "ErrSymlinkInArchive can be wrapped",
			sentinel: cluster.ErrSymlinkInArchive,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrapped := fmt.Errorf("context: %w", testCase.sentinel)
			assert.ErrorIs(t, wrapped, testCase.sentinel)
		})
	}
}

// TestNewRestoreCmd_FlagsExistWithCorrectDefaults verifies that NewRestoreCmd
// registers all expected flags with the correct default values.
func TestNewRestoreCmd_FlagsExistWithCorrectDefaults(t *testing.T) {
	t.Parallel()

	flagTests := []struct {
		flagName     string
		shorthand    string
		defaultValue string
	}{
		{
			flagName:     "input",
			shorthand:    "i",
			defaultValue: "",
		},
		{
			flagName:     "existing-resource-policy",
			shorthand:    "",
			defaultValue: "none",
		},
		{
			flagName:     "dry-run",
			shorthand:    "",
			defaultValue: "false",
		},
	}

	for _, flagTest := range flagTests {
		t.Run(flagTest.flagName, func(t *testing.T) {
			t.Parallel()

			restoreCmd := cluster.NewRestoreCmd(nil)
			require.NotNil(t, restoreCmd)

			flag := restoreCmd.Flags().Lookup(flagTest.flagName)
			require.NotNil(t, flag, "flag %q should be registered", flagTest.flagName)
			assert.Equal(t, flagTest.defaultValue, flag.DefValue,
				"flag %q should have default value %q", flagTest.flagName, flagTest.defaultValue)

			if flagTest.shorthand != "" {
				assert.Equal(t, flagTest.shorthand, flag.Shorthand,
					"flag %q should have shorthand %q", flagTest.flagName, flagTest.shorthand)
			}
		})
	}
}

// TestNewRestoreCmd_InputFlagIsRequired verifies that --input is marked required.
func TestNewRestoreCmd_InputFlagIsRequired(t *testing.T) {
	t.Parallel()

	restoreCmd := cluster.NewRestoreCmd(nil)
	require.NotNil(t, restoreCmd)

	restoreCmd.SetOut(io.Discard)
	restoreCmd.SetErr(io.Discard)
	restoreCmd.SetArgs([]string{"--existing-resource-policy", "none"})

	err := restoreCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "input")
}

// TestRestoreCmd_InvalidResourcePolicy verifies that an invalid
// existing-resource-policy value returns ErrInvalidResourcePolicy.
// The policy validation in runRestore happens before kubeconfig and
// file access, so we do not need a real cluster or archive for this test.
func TestRestoreCmd_InvalidResourcePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy string
	}{
		{name: "unknown policy value", policy: "unknown"},
		{name: "capitalised none", policy: "None"},
		{name: "capitalised update", policy: "Update"},
		{name: "unsupported policy value 'skip'", policy: "skip"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			restoreCmd := cluster.NewRestoreCmd(nil)
			restoreCmd.SetOut(io.Discard)
			restoreCmd.SetErr(io.Discard)
			restoreCmd.SetArgs([]string{
				"--input", "dummy.tar.gz",
				"--existing-resource-policy", testCase.policy,
			})

			err := restoreCmd.Execute()

			require.Error(t, err)
			assert.ErrorIs(t, err, cluster.ErrInvalidResourcePolicy,
				"expected ErrInvalidResourcePolicy, got: %v", err,
			)
		})
	}
}

// TestRestoreCmd_ValidPoliciesPassValidation verifies that "none" and "update"
// are accepted as valid policy values. The command will fail later when trying
// to open the nonexistent --input archive, NOT at the policy check.
func TestRestoreCmd_ValidPoliciesPassValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy string
	}{
		{name: "none policy", policy: "none"},
		{name: "update policy", policy: "update"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			nonexistentArchive := filepath.Join(t.TempDir(), "nonexistent.tar.gz")

			restoreCmd := cluster.NewRestoreCmd(nil)
			restoreCmd.SetOut(io.Discard)
			restoreCmd.SetErr(io.Discard)
			restoreCmd.SetArgs([]string{
				"--input", nonexistentArchive,
				"--existing-resource-policy", testCase.policy,
			})

			err := restoreCmd.Execute()

			require.Error(
				t, err,
				"expected a later error (archive not found), not ErrInvalidResourcePolicy",
			)
			assert.NotErrorIs(t, err, cluster.ErrInvalidResourcePolicy,
				"valid policy %q should not return ErrInvalidResourcePolicy", testCase.policy,
			)
		})
	}
}

// TestRestoreCmd_Metadata verifies basic command metadata.
func TestRestoreCmd_Metadata(t *testing.T) {
	t.Parallel()

	restoreCmd := cluster.NewRestoreCmd(nil)
	require.NotNil(t, restoreCmd)

	assert.Equal(t, "restore", restoreCmd.Use)
	assert.NotEmpty(t, restoreCmd.Short)
	assert.NotEmpty(t, restoreCmd.Long)
	assert.True(t, restoreCmd.SilenceUsage)
}

// TestDeriveBackupName_ExtensionStripping verifies the extension stripping
// logic for .tar.gz and .tgz archives.
func TestDeriveBackupName_ExtensionStripping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tar.gz with path",
			input:    "/backups/cluster-backup.tar.gz",
			expected: "cluster-backup",
		},
		{
			name:     "tgz with path",
			input:    "/backups/cluster-backup.tgz",
			expected: "cluster-backup",
		},
		{
			name:     "simple filename",
			input:    "my-backup.tar.gz",
			expected: "my-backup",
		},
		{
			name:     "no extension",
			input:    "my-backup",
			expected: "my-backup",
		},
		{
			name:     "other extension preserved",
			input:    "my-backup.zip",
			expected: "my-backup.zip",
		},
		{
			name:     "timestamped name",
			input:    "/mnt/ksail-backup-2026-03-21T10:00:00Z.tar.gz",
			expected: "ksail-backup-2026-03-21T10:00:00Z",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportDeriveBackupName(testCase.input)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestAllLinesContain_EdgeCases tests additional edge cases for the
// allLinesContain helper used in restore's "already exists" detection.

//nolint:dupl
func TestAllLinesContain_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		substr   string
		expected bool
	}{
		{
			name:     "single matching non-empty line",
			output:   "already exists",
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "line with surrounding whitespace",
			output:   "  already exists  \n",
			substr:   "already exists",
			expected: true,
		},
		{
			name:     "mixed matching and non-matching",
			output:   "already exists\nother error",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "completely empty output",
			output:   "",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "all empty lines",
			output:   "\n\n\n",
			substr:   "already exists",
			expected: false,
		},
		{
			name:     "multiple all-matching lines",
			output:   "error: resource already exists\nerror: configmap already exists\nerror: secret already exists",
			substr:   "already exists",
			expected: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportAllLinesContain(testCase.output, testCase.substr)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestPrintRestoreHeader verifies that printRestoreHeader writes the expected
// lines including the input path, policy, and (when dry-run) the dry-run note.
func TestPrintRestoreHeader( //nolint:funlen // Table-driven test with multiple comprehensive cases
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name      string
		inputPath string
		policy    string
		dryRun    bool
		wantLines []string
		noLines   []string
	}{
		{
			name:      "standard header without dry-run",
			inputPath: "/backups/cluster.tar.gz",
			policy:    "none",
			dryRun:    false,
			wantLines: []string{
				"Starting cluster restore",
				"/backups/cluster.tar.gz",
				"none",
				"Extracting backup archive",
			},
			noLines: []string{"dry-run"},
		},
		{
			name:      "header with dry-run enabled",
			inputPath: "/backups/cluster.tar.gz",
			policy:    "update",
			dryRun:    true,
			wantLines: []string{
				"Starting cluster restore",
				"/backups/cluster.tar.gz",
				"update",
				"dry-run",
			},
		},
		{
			name:      "header with update policy",
			inputPath: "relative/path/backup.tar.gz",
			policy:    "update",
			dryRun:    false,
			wantLines: []string{
				"update",
				"relative/path/backup.tar.gz",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cluster.ExportPrintRestoreHeader(
				&buf, testCase.inputPath, testCase.policy, testCase.dryRun,
			)
			output := buf.String()

			for _, want := range testCase.wantLines {
				assert.Contains(t, output, want,
					"output should contain %q", want)
			}

			for _, noWant := range testCase.noLines {
				assert.NotContains(t, output, noWant,
					"output should not contain %q", noWant)
			}
		})
	}
}

// TestPrintRestoreMetadata verifies that printRestoreMetadata correctly outputs
// all metadata fields, including optional Distribution and Provider.
func TestPrintRestoreMetadata( //nolint:funlen // Table-driven test with multiple comprehensive cases
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name      string
		metadata  *cluster.BackupMetadata
		wantLines []string
		noLines   []string
	}{
		{
			name: "full metadata with distribution and provider",
			metadata: &cluster.BackupMetadata{
				Version:       "v1",
				Timestamp:     time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
				ClusterName:   "my-cluster",
				Distribution:  "Vanilla",
				Provider:      "Docker",
				ResourceCount: 42,
			},
			wantLines: []string{
				"v1",
				"2026-03-15",
				"my-cluster",
				"Vanilla",
				"Docker",
				"42",
			},
		},
		{
			name: "metadata without optional distribution and provider",
			metadata: &cluster.BackupMetadata{
				Version:       "v1",
				Timestamp:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				ClusterName:   "bare-cluster",
				ResourceCount: 5,
			},
			wantLines: []string{
				"v1",
				"bare-cluster",
				"5",
			},
			noLines: []string{"Distribution:", "Provider:"},
		},
		{
			name: "zero resource count is printed",
			metadata: &cluster.BackupMetadata{
				Version:     "v1",
				ClusterName: "empty-cluster",
			},
			wantLines: []string{"Resources: 0"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cluster.ExportPrintRestoreMetadata(&buf, testCase.metadata)
			output := buf.String()

			for _, want := range testCase.wantLines {
				assert.Contains(t, output, want,
					"output should contain %q", want)
			}

			for _, noWant := range testCase.noLines {
				assert.NotContains(t, output, noWant,
					"output should not contain %q", noWant)
			}
		})
	}
}

// TestReadBackupMetadata verifies error paths and happy path of readBackupMetadata.
func TestReadBackupMetadata( //nolint:funlen // Covers multiple distinct error and success paths
	t *testing.T,
) {
	t.Parallel()

	t.Run("returns error when metadata file is missing", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		_, err := cluster.ExportReadBackupMetadata(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup metadata")
	})

	t.Run("returns error when metadata is not valid JSON", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err := os.WriteFile(metaPath, []byte("{not valid json"), 0o600)
		require.NoError(t, err)

		_, err = cluster.ExportReadBackupMetadata(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse backup metadata")
	})

	t.Run("returns metadata when file is valid JSON", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		meta := &cluster.BackupMetadata{
			Version:       "v1",
			ClusterName:   "test",
			ResourceCount: 7,
		}

		data, err := json.Marshal(meta)
		require.NoError(t, err)

		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err = os.WriteFile(metaPath, data, 0o600)
		require.NoError(t, err)

		result, err := cluster.ExportReadBackupMetadata(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, "v1", result.Version)
		assert.Equal(t, "test", result.ClusterName)
		assert.Equal(t, 7, result.ResourceCount)
	})

	t.Run("empty JSON object is parsed without error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		metaPath := filepath.Join(tmpDir, "backup-metadata.json")
		err := os.WriteFile(metaPath, []byte("{}"), 0o600)
		require.NoError(t, err)

		result, err := cluster.ExportReadBackupMetadata(tmpDir)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.Version)
	})
}

// TestExtractBackupArchive_ErrorPaths validates error handling when the
// archive is missing, corrupt, or lacks the expected metadata file.
func TestExtractBackupArchive_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent archive returns error", func(t *testing.T) {
		t.Parallel()

		_, _, err := cluster.ExportExtractBackupArchive(
			filepath.Join(t.TempDir(), "does-not-exist.tar.gz"),
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup archive")
	})

	t.Run("non-gzip file returns gzip error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		badFile := filepath.Join(tmpDir, "bad.tar.gz")
		err := os.WriteFile(badFile, []byte("this is not gzip content"), 0o600)
		require.NoError(t, err)

		_, _, err = cluster.ExportExtractBackupArchive(badFile)
		require.Error(t, err)
	})

	t.Run("valid gzip but empty content returns tar error", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "empty.tar.gz")

		archiveFile, err := os.Create(archivePath) //nolint:gosec // test-controlled temp path
		require.NoError(t, err)

		gz := gzip.NewWriter(archiveFile)
		// Write a single byte so gzip stream is valid but not a valid tar archive
		_, err = gz.Write([]byte{0x00})
		require.NoError(t, err)
		require.NoError(t, gz.Close())
		require.NoError(t, archiveFile.Close())

		_, _, err = cluster.ExportExtractBackupArchive(archivePath)
		require.Error(t, err)
	})

	t.Run("valid tar.gz without metadata returns error", func(t *testing.T) {
		t.Parallel()

		archivePath := createArchiveWithoutMetadata(t)

		_, _, err := cluster.ExportExtractBackupArchive(archivePath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup metadata")
	})
}

// createArchiveWithoutMetadata creates a valid .tar.gz file that contains
// a single YAML file but no backup-metadata.json entry.
func createArchiveWithoutMetadata(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "no-meta.tar.gz")

	f, err := os.Create(archivePath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	gzipWriter := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gzipWriter)

	content := []byte("apiVersion: v1\nkind: Pod\n")
	hdr := &tar.Header{
		Name:     "resources/pods.yaml",
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o600,
	}

	err = tarWriter.WriteHeader(hdr)
	require.NoError(t, err)

	_, err = tarWriter.Write(content)
	require.NoError(t, err)

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())

	return archivePath
}

// TestExtractBackupArchive_HappyPath validates that a well-formed .tar.gz
// archive is correctly extracted and its metadata is returned.
func TestExtractBackupArchive_HappyPath(t *testing.T) {
	t.Parallel()

	archivePath := createValidArchive(t)

	tmpDir, meta, err := cluster.ExportExtractBackupArchive(archivePath)
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tmpDir) }()

	require.NotNil(t, meta)
	assert.Equal(t, "v1", meta.Version)
	assert.Equal(t, "test-cluster", meta.ClusterName)
	assert.Equal(t, 3, meta.ResourceCount)

	// The resources directory must exist inside the extracted temp dir.
	resourcesDir := filepath.Join(tmpDir, "resources")
	_, err = os.Stat(resourcesDir)
	require.NoError(t, err, "resources directory should be extracted")

	// The YAML file must be present.
	podFile := filepath.Join(resourcesDir, "pods.yaml")
	_, err = os.Stat(podFile)
	require.NoError(t, err, "pods.yaml should be extracted")
}

// createValidArchive builds a complete, valid backup .tar.gz that passes
// extractBackupArchive (includes backup-metadata.json and a resource file).
func createValidArchive(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "valid.tar.gz")

	f, err := os.Create(archivePath) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	gzipWriter := gzip.NewWriter(f)
	tarWriter := tar.NewWriter(gzipWriter)

	// Add backup-metadata.json.
	meta := `{"version":"v1","clusterName":"test-cluster","resourceCount":3}`
	addTarEntry(t, tarWriter, "backup-metadata.json", []byte(meta))

	// Add a resources directory entry.
	err = tarWriter.WriteHeader(&tar.Header{
		Name:     "resources/",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	})
	require.NoError(t, err)

	// Add a YAML resource file.
	addTarEntry(t, tarWriter, "resources/pods.yaml", []byte("apiVersion: v1\nkind: Pod\n"))

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())

	return archivePath
}

// addTarEntry writes a regular file entry into the tar writer.
func addTarEntry(t *testing.T, tarWriter *tar.Writer, name string, content []byte) {
	t.Helper()

	err := tarWriter.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o600,
	})
	require.NoError(t, err)

	_, err = tarWriter.Write(content)
	require.NoError(t, err)
}

// TestBackupResourceTypes verifies that backupResourceTypes returns a non-empty
// ordered slice and that CRDs appear before namespaces (dependency ordering).
func TestBackupResourceTypes(t *testing.T) {
	t.Parallel()

	types := cluster.ExportBackupResourceTypes()

	require.NotEmpty(t, types, "backupResourceTypes must return a non-empty list")

	// Validate that the list contains well-known resource types.
	typeSet := make(map[string]struct{}, len(types))
	for _, rt := range types {
		typeSet[rt] = struct{}{}
	}

	for _, expected := range []string{
		"customresourcedefinitions",
		"namespaces",
		"deployments",
	} {
		assert.Contains(t, typeSet, expected,
			"backupResourceTypes should include %q", expected)
	}

	// CRDs must come before namespaces in restore ordering.
	crdIdx, nsIdx := -1, -1

	for i, rt := range types {
		switch rt {
		case "customresourcedefinitions":
			crdIdx = i
		case "namespaces":
			nsIdx = i
		}
	}

	assert.Greater(t, nsIdx, crdIdx,
		"customresourcedefinitions must appear before namespaces in resource ordering")

	// No duplicates allowed.
	seen := make(map[string]bool, len(types))
	for _, rt := range types {
		assert.False(t, seen[rt], "duplicate resource type %q in backupResourceTypes", rt)
		seen[rt] = true
	}

	// Every entry must be a non-empty, lowercase string without whitespace.
	for _, rt := range types {
		assert.NotEmpty(t, rt)
		assert.Equal(t, strings.ToLower(rt), rt,
			"resource type %q should be lowercase", rt)
		assert.NotContains(t, rt, " ",
			"resource type %q should not contain spaces", rt)
	}
}

const testKubeconfigTwoContexts = `apiVersion: v1
kind: Config
current-context: kind-dev
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-dev
- cluster:
    server: https://127.0.0.1:7443
  name: kind-staging
contexts:
- context:
    cluster: kind-dev
    user: kind-dev
  name: kind-dev
- context:
    cluster: kind-staging
    user: kind-staging
  name: kind-staging
users:
- name: kind-dev
  user: {}
- name: kind-staging
  user: {}
`

const testKubeconfigMultiDistro = `apiVersion: v1
kind: Config
current-context: kind-myapp
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-myapp
- cluster:
    server: https://127.0.0.1:7443
  name: k3d-myapp
contexts:
- context:
    cluster: kind-myapp
    user: kind-myapp
  name: kind-myapp
- context:
    cluster: k3d-myapp
    user: k3d-myapp
  name: k3d-myapp
users:
- name: kind-myapp
  user: {}
- name: k3d-myapp
  user: {}
`

func newSwitchTestCmd() (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "switch"}

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetContext(context.Background())

	return cmd, &buf
}

func TestSwitchCmd_HappyPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "staging", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"Switched to cluster 'staging' (context: kind-staging)")

	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "kind-staging", config.CurrentContext)
}

func TestSwitchCmd_K3sDistribution(t *testing.T) {
	t.Parallel()

	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: k3d-prod
contexts:
- context:
    cluster: k3d-prod
    user: k3d-prod
  name: k3d-prod
users:
- name: k3d-prod
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "prod", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"context: k3d-prod")

	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "k3d-prod", config.CurrentContext)
}

func TestSwitchCmd_TalosDistribution(t *testing.T) {
	t.Parallel()

	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: talos-cluster
contexts:
- context:
    cluster: talos-cluster
    user: admin@talos-cluster
  name: admin@talos-cluster
users:
- name: admin@talos-cluster
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "talos-cluster", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"context: admin@talos-cluster")

	//nolint:gosec // G304: test-controlled path from t.TempDir()
	updatedBytes, err := os.ReadFile(kubeconfigPath)
	require.NoError(t, err)

	config, err := clientcmd.Load(updatedBytes)
	require.NoError(t, err)

	assert.Equal(t, "admin@talos-cluster", config.CurrentContext)
}

func TestSwitchCmd_ContextNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "nonexistent", deps)
	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrContextNotFound)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "available contexts")
}

func TestSwitchCmd_AmbiguousCluster(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigMultiDistro),
		0o600,
	))

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "myapp", deps)
	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrAmbiguousCluster)
	assert.Contains(t, err.Error(), "k3d-myapp")
	assert.Contains(t, err.Error(), "kind-myapp")
}

func TestSwitchCmd_KubeconfigNotFound(t *testing.T) {
	t.Parallel()

	cmd, _ := newSwitchTestCmd()

	deps := cluster.SwitchDeps{
		KubeconfigPath: "/nonexistent/path/kubeconfig",
	}

	err := cluster.HandleSwitchRunE(cmd, "some-cluster", deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

func TestSwitchCmd_SameContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{KubeconfigPath: kubeconfigPath}

	err := cluster.HandleSwitchRunE(cmd, "dev", deps)
	require.NoError(t, err)

	assert.Contains(t, buf.String(),
		"Switched to cluster 'dev'")
}

func TestSwitchCmd_InteractivePicker(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	cmd, buf := newSwitchTestCmd()

	deps := cluster.SwitchDeps{
		KubeconfigPath: kubeconfigPath,
		PickCluster: func(_ string, items []string) (string, error) {
			// Simulate user selecting "staging"
			for _, item := range items {
				if item == "staging" {
					return item, nil
				}
			}

			return "", fmt.Errorf("staging not in list: %w", cluster.ErrNoClusters)
		},
	}

	clusterName, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)
	assert.Equal(t, "staging", clusterName)

	err = cluster.HandleSwitchRunE(cmd, clusterName, deps)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Switched to cluster 'staging'")
}

func TestSwitchCmd_InteractivePicker_NoClusters(t *testing.T) {
	t.Parallel()

	// Kubeconfig with no KSail-managed contexts (no known distribution prefix)
	kubeconfig := `apiVersion: v1
kind: Config
current-context: ""
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: unknown-cluster
contexts:
- context:
    cluster: unknown-cluster
    user: some-user
  name: unknown-cluster
users:
- name: some-user
  user: {}
`

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath, []byte(kubeconfig), 0o600,
	))

	cmd, _ := newSwitchTestCmd()

	deps := cluster.SwitchDeps{
		KubeconfigPath: kubeconfigPath,
	}

	_, err := cluster.ExportPickCluster(cmd, deps)
	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrNoClusters)
}

func TestSwitchCmd_FallbackKubeconfigFromEnv(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	t.Setenv("KUBECONFIG", kubeconfigPath)

	cmd, buf := newSwitchTestCmd()
	deps := cluster.SwitchDeps{}

	err := cluster.HandleSwitchRunE(cmd, "staging", deps)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Switched to cluster 'staging'")
}

func TestSwitchCmd_FallbackKubeconfigFromEnv_PickCluster(t *testing.T) {
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")

	require.NoError(t, os.WriteFile(
		kubeconfigPath,
		[]byte(testKubeconfigTwoContexts),
		0o600,
	))

	t.Setenv("KUBECONFIG", kubeconfigPath)

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{
		PickCluster: func(_ string, items []string) (string, error) {
			for _, item := range items {
				if item == "staging" {
					return item, nil
				}
			}

			return "", fmt.Errorf("staging not in list: %w", cluster.ErrNoClusters)
		},
	}

	clusterName, err := cluster.ExportPickCluster(cmd, deps)
	require.NoError(t, err)
	assert.Equal(t, "staging", clusterName)
}

func TestSwitchCmd_FallbackKubeconfigInvalid(t *testing.T) {
	t.Setenv("KUBECONFIG", "/nonexistent/invalid/kubeconfig")

	cmd, _ := newSwitchTestCmd()
	deps := cluster.SwitchDeps{}

	err := cluster.HandleSwitchRunE(cmd, "staging", deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read kubeconfig")
}

func TestDisplayListResults_WithTTL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	results := []cluster.ExportListResult{
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionVanilla,
			"cluster-1",
			nil,
		),
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionK3s,
			"dev-cluster",
			// Use 5h buffer so minute-boundary drift on slow CI is irrelevant.
			&state.TTLInfo{ExpiresAt: time.Now().Add(5*time.Hour + 30*time.Second)},
		),
	}

	cluster.ExportDisplayListResults(&buf, []v1alpha1.Provider{v1alpha1.ProviderDocker}, results)

	output := buf.String()
	assert.Contains(t, output, "PROVIDER")
	assert.Contains(t, output, "DISTRIBUTION")
	assert.Contains(t, output, "CLUSTER")
	assert.Contains(t, output, "TTL")
	assert.Contains(t, output, "cluster-1")
	assert.Contains(t, output, "dev-cluster")
	assert.Regexp(t, `\d+h`, output)
}

func TestDisplayListResults_WithExpiredTTL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	results := []cluster.ExportListResult{
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionTalos,
			"expired-cluster",
			&state.TTLInfo{ExpiresAt: time.Now().Add(-1 * time.Hour)},
		),
	}

	cluster.ExportDisplayListResults(&buf, []v1alpha1.Provider{v1alpha1.ProviderDocker}, results)

	output := buf.String()
	assert.Contains(t, output, "TTL")
	assert.Contains(t, output, "EXPIRED")
	assert.Contains(t, output, "expired-cluster")
}

func TestDisplayListResults_NoTTLColumn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	results := []cluster.ExportListResult{
		cluster.ExportNewListResult(
			v1alpha1.ProviderDocker,
			v1alpha1.DistributionVanilla,
			"my-cluster",
			nil,
		),
	}

	cluster.ExportDisplayListResults(&buf, []v1alpha1.Provider{v1alpha1.ProviderDocker}, results)

	output := buf.String()
	assert.Contains(t, output, "PROVIDER")
	assert.Contains(t, output, "CLUSTER")
	assert.NotContains(t, output, "TTL")
}

func TestStripParenthetical_NoSuffix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kind", cluster.ExportStripParenthetical("kind"))
}

func TestStripParenthetical_WithSuffix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kind", cluster.ExportStripParenthetical("kind (Vanilla)"))
}

func TestStripParenthetical_EmptyString(t *testing.T) {
	t.Parallel()

	assert.Empty(t, cluster.ExportStripParenthetical(""))
}

func TestStripParenthetical_NoClosingParen(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "kind (Vanilla", cluster.ExportStripParenthetical("kind (Vanilla"))
}

func TestFormatRemainingDuration_HoursAndMinutes(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(1*time.Hour + 23*time.Minute)
	assert.Equal(t, "1h 23m", result)
}

func TestFormatRemainingDuration_HoursOnly(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(2 * time.Hour)
	assert.Equal(t, "2h", result)
}

func TestFormatRemainingDuration_MinutesOnly(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(45 * time.Minute)
	assert.Equal(t, "45m", result)
}

func TestFormatRemainingDuration_SubMinute(t *testing.T) {
	t.Parallel()

	result := cluster.ExportFormatRemainingDuration(59 * time.Second)
	assert.Equal(t, "<1m", result)
}

func TestFormatRemainingDuration_TruncatesDown(t *testing.T) {
	t.Parallel()

	// 1h 23m 59s should truncate to 1h 23m, never round up to 1h 24m.
	result := cluster.ExportFormatRemainingDuration(
		1*time.Hour + 23*time.Minute + 59*time.Second,
	)
	assert.Equal(t, "1h 23m", result)
}

func TestMaybeWaitForTTL_EmptyFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("ttl", "", "")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetContext(context.Background())

	clusterCfg := &v1alpha1.Cluster{}

	err := cluster.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
	require.NoError(t, err)
}

func TestMaybeWaitForTTL_InvalidDuration(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("ttl", "", "")
	_ = cmd.Flags().Set("ttl", "not-a-duration")

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	clusterCfg := &v1alpha1.Cluster{}

	// Invalid duration should not block or attempt deletion; returns nil.
	err := cluster.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "invalid --ttl value")
}

func TestMaybeWaitForTTL_NonPositiveDuration(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("ttl", "", "")
	_ = cmd.Flags().Set("ttl", "-1h")

	var buf bytes.Buffer

	cmd.SetOut(&buf)
	cmd.SetContext(context.Background())

	clusterCfg := &v1alpha1.Cluster{}

	// Non-positive duration should return immediately without blocking or TTL setup.
	err := cluster.ExportMaybeWaitForTTL(cmd, "test-cluster", clusterCfg)
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "non-positive TTL should produce no output")
}

// Package-level sink prevents the compiler from optimizing away benchmark calls.
//
//nolint:gochecknoglobals // Benchmark sink variable is required to prevent compiler optimization.
var benchFormatDiffTableSink string

// newInPlaceDiff builds an UpdateResult with count in-place changes.
func newInPlaceDiff(count int) *clusterupdate.UpdateResult {
	result := clusterupdate.NewEmptyUpdateResult()

	fields := []struct{ field, old, new string }{
		{"cluster.cni", "Default", "Cilium"},
		{"cluster.csi", "Default", "Enabled"},
		{"cluster.metricsServer", "Default", "Enabled"},
		{"cluster.loadBalancer", "Default", "Enabled"},
		{"cluster.certManager", "Disabled", "Enabled"},
		{"cluster.policyEngine", "None", "Kyverno"},
		{"cluster.gitOpsEngine", "None", "Flux"},
		{"cluster.localRegistry.registry", "", "localhost:5050"},
		{"cluster.talos.workers", "0", "2"},
		{"provider.hetzner.sshKeyName", "old-key", "new-key"},
	}

	for i := range count {
		idx := i % len(fields)
		result.InPlaceChanges = append(result.InPlaceChanges, clusterupdate.Change{
			Field:    fields[idx].field,
			OldValue: fields[idx].old,
			NewValue: fields[idx].new,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "component can be switched via Helm",
		})
	}

	return result
}

// newMixedDiff builds an UpdateResult with changes across all three categories.
func newMixedDiff() *clusterupdate.UpdateResult {
	result := clusterupdate.NewEmptyUpdateResult()

	result.RecreateRequired = []clusterupdate.Change{
		{
			Field:    "cluster.distribution",
			OldValue: "Vanilla",
			NewValue: "K3s",
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "changing distribution requires recreation",
		},
		{
			Field:    "cluster.localRegistry.registry",
			OldValue: "localhost:5050",
			NewValue: "localhost:6060",
			Category: clusterupdate.ChangeCategoryRecreateRequired,
			Reason:   "Kind requires recreate for registry changes",
		},
	}
	result.RebootRequired = []clusterupdate.Change{
		{
			Field:    "cluster.talos.iso",
			OldValue: "122630",
			NewValue: "999999",
			Category: clusterupdate.ChangeCategoryRebootRequired,
			Reason:   "ISO change affects provisioned nodes",
		},
	}
	result.InPlaceChanges = []clusterupdate.Change{
		{
			Field:    "cluster.cni",
			OldValue: "Default",
			NewValue: "Cilium",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "CNI can be switched via Helm",
		},
		{
			Field:    "cluster.policyEngine",
			OldValue: "None",
			NewValue: "Kyverno",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "policy engine can be switched via Helm",
		},
	}

	return result
}

// BenchmarkFormatDiffTable_SingleChange measures table formatting with exactly
// one in-place change. This is the smallest realistic non-zero input.
func BenchmarkFormatDiffTable_SingleChange(b *testing.B) {
	diff := newInPlaceDiff(1)
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = cluster.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_SmallDiff measures table formatting with a typical
// small diff (3 in-place changes). This represents a common incremental update.
func BenchmarkFormatDiffTable_SmallDiff(b *testing.B) {
	diff := newInPlaceDiff(3)
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = cluster.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_MixedCategories measures table formatting with
// changes across all three severity categories (recreate, reboot, in-place).
func BenchmarkFormatDiffTable_MixedCategories(b *testing.B) {
	diff := newMixedDiff()
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = cluster.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_LargeDiff measures table formatting with many
// changes (10 rows). This stress-tests column-width computation and
// strings.Builder pre-allocation sizing.
func BenchmarkFormatDiffTable_LargeDiff(b *testing.B) {
	diff := newInPlaceDiff(10)
	total := diff.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = cluster.ExportFormatDiffTable(diff, total)
	}
}

// BenchmarkFormatDiffTable_WideValues measures formatting overhead when
// field names and values are longer than the column headers, exercising
// the dynamic column-width computation path.
func BenchmarkFormatDiffTable_WideValues(b *testing.B) {
	result := clusterupdate.NewEmptyUpdateResult()
	result.InPlaceChanges = []clusterupdate.Change{
		{
			Field:    "provider.hetzner.controlPlaneServerType",
			OldValue: "cx23",
			NewValue: "cx53",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "new worker servers will use the new type; existing workers unchanged",
		},
		{
			Field:    "provider.hetzner.networkName",
			OldValue: "legacy-network-name",
			NewValue: "production-network-name",
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "cannot migrate servers between networks",
		},
	}

	total := result.TotalChanges()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		benchFormatDiffTableSink = cluster.ExportFormatDiffTable(result, total)
	}
}

func TestNewUpdateCmd(t *testing.T) { //nolint:cyclop // flag assertion test
	t.Parallel()

	runtimeContainer := &di.Runtime{}
	cmd := cluster.NewUpdateCmd(runtimeContainer)

	// Verify command basics
	if cmd.Use != "update" {
		t.Errorf("expected Use to be 'update', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	if cmd.Long == "" {
		t.Error("expected Long description to be set")
	}

	// Verify flags
	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("expected --force flag to exist")
	}

	nameFlag := cmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Error("expected --name flag to exist")
	}

	mirrorRegistryFlag := cmd.Flags().Lookup("mirror-registry")
	if mirrorRegistryFlag == nil {
		t.Error("expected --mirror-registry flag to exist")
	}

	dryRunFlag := cmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Error("expected --dry-run flag to exist")
	}

	yesFlag := cmd.Flags().Lookup("yes")
	if yesFlag == nil {
		t.Error("expected --yes flag to exist")
	}

	updateK8sFlag := cmd.Flags().Lookup("update-kubernetes")
	if updateK8sFlag == nil {
		t.Error("expected --update-kubernetes flag to exist")
	} else if updateK8sFlag.DefValue != "false" {
		t.Errorf("expected --update-kubernetes default to be false, got %q", updateK8sFlag.DefValue)
	}

	updateDistFlag := cmd.Flags().Lookup("update-distribution")
	if updateDistFlag == nil {
		t.Error("expected --update-distribution flag to exist")
	} else if updateDistFlag.DefValue != "false" {
		t.Errorf(
			"expected --update-distribution default to be false, got %q",
			updateDistFlag.DefValue,
		)
	}
}

func TestResolveForce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		forceValue bool
		yesValue   string
		expected   bool
	}{
		{name: "--force resolves to true", forceValue: true, yesValue: "", expected: true},
		{name: "--yes resolves to true", forceValue: false, yesValue: "true", expected: true},
		{
			name:       "--yes=false resolves to false",
			forceValue: false,
			yesValue:   "false",
			expected:   false,
		},
		{name: "both flags resolve to true", forceValue: true, yesValue: "true", expected: true},
		{name: "neither flag resolves to false", forceValue: false, yesValue: "", expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			runtimeContainer := &di.Runtime{}
			cmd := cluster.NewUpdateCmd(runtimeContainer)

			if testCase.yesValue != "" {
				_ = cmd.Flags().Set("yes", testCase.yesValue)
			}

			result := cluster.ExportResolveForce(testCase.forceValue, cmd.Flags().Lookup("yes"))
			if result != testCase.expected {
				t.Errorf("expected resolveForce(%v, yes=%q) = %v, got %v",
					testCase.forceValue, testCase.yesValue, testCase.expected, result)
			}
		})
	}
}

//nolint:paralleltest // subtests override global stdin reader
func TestUpdateConfirmation_UsesConfirmPackage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "user confirms with 'yes'",
			input:    "yes\n",
			expected: true,
		},
		{
			name:     "user confirms with 'YES'",
			input:    "YES\n",
			expected: true,
		},
		{
			name:     "user rejects with 'no'",
			input:    "no\n",
			expected: false,
		},
		{
			name:     "user rejects with empty input",
			input:    "\n",
			expected: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel: SetStdinReaderForTests overrides a process-wide global
			restore := confirm.SetStdinReaderForTests(strings.NewReader(testCase.input))
			defer restore()

			result := confirm.PromptForConfirmation(nil)

			if result != testCase.expected {
				t.Errorf("expected %v, got %v", testCase.expected, result)
			}
		})
	}
}

//nolint:paralleltest // subtests override global TTY checker
func TestUpdateConfirmation_ShouldSkipPrompt(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		isTTY    bool
		expected bool
	}{
		{name: "force skips prompt", force: true, isTTY: true, expected: true},
		{name: "force skips even non-TTY", force: true, isTTY: false, expected: true},
		{name: "non-TTY skips prompt", force: false, isTTY: false, expected: true},
		{name: "TTY without force shows prompt", force: false, isTTY: true, expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			// Not parallel: SetTTYCheckerForTests overrides a process-wide global
			restore := confirm.SetTTYCheckerForTests(func() bool {
				return testCase.isTTY
			})
			defer restore()

			result := confirm.ShouldSkipPrompt(testCase.force)
			if result != testCase.expected {
				t.Errorf("expected ShouldSkipPrompt(%v) = %v, got %v",
					testCase.force, testCase.expected, result)
			}
		})
	}
}

func TestDisplayChangesSummary_NoChanges(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportDisplayChangesSummary(cmd, diff)

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty diff, got %q", buf.String())
	}
}

func TestDisplayChangesSummary_TableFormat(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "CNI can be switched via Helm",
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change requires recreation",
	})

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()

	expected := []struct {
		label string
		text  string
	}{
		{"Component header", "Component"},
		{"Before header", "Before"},
		{"After header", "After"},
		{"Impact header", "Impact"},
		{"in-place icon", "🟢"},
		{"recreate-required icon", "🔴"},
		{"in-place label", "in-place"},
		{"recreate-required label", "recreate-required"},
		{"cluster.cni field", "cluster.cni"},
		{"cluster.distribution field", "cluster.distribution"},
		{"Cilium value", "Cilium"},
		{"Talos value", "Talos"},
		{"change count summary", "Detected 2 configuration changes"},
		{"separator line", "─"},
	}

	for _, entry := range expected {
		if !strings.Contains(output, entry.text) {
			t.Errorf("expected output to contain %s (%q)", entry.label, entry.text)
		}
	}
}

func TestDisplayChangesSummary_SeverityOrder(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
	})
	diff.RebootRequired = append(diff.RebootRequired, clusterupdate.Change{
		Field:    "talos.kernel_args",
		OldValue: "",
		NewValue: "console=ttyS0",
		Category: clusterupdate.ChangeCategoryRebootRequired,
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
	})

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()

	// Recreate-required (🔴) should appear before reboot-required (🟡)
	// and reboot-required should appear before in-place (🟢)
	idxRecreate := strings.Index(output, "🔴")
	idxReboot := strings.Index(output, "🟡")
	idxInPlace := strings.Index(output, "🟢")

	if idxRecreate < 0 || idxReboot < 0 || idxInPlace < 0 {
		t.Fatal("expected all three icons to be present")
	}

	if idxRecreate > idxReboot {
		t.Error("recreate-required should appear before reboot-required")
	}

	if idxReboot > idxInPlace {
		t.Error("reboot-required should appear before in-place")
	}
}

func TestNewUpdateCmd_HasOutputFlag(t *testing.T) {
	t.Parallel()

	runtimeContainer := &di.Runtime{}
	cmd := cluster.NewUpdateCmd(runtimeContainer)

	outputFlag := cmd.Flags().Lookup("output")
	if outputFlag == nil {
		t.Fatal("expected --output flag to exist")
	}

	if outputFlag.DefValue != "text" {
		t.Errorf("expected --output default to be 'text', got %q", outputFlag.DefValue)
	}
}

func TestDisplayChangesSummary_JSONOutput(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer

	cmd.SetOut(&buf)

	cmd.Flags().String("output", "json", "")

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "CNI can be switched via Helm",
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change requires recreation",
	})

	cluster.ExportDisplayChangesSummary(cmd, diff)

	output := buf.String()

	if !strings.Contains(output, `"totalChanges"`) {
		t.Errorf("expected JSON output to contain totalChanges key, got: %q", output)
	}

	if !strings.Contains(output, `"inPlaceChanges"`) {
		t.Errorf("expected JSON output to contain inPlaceChanges key, got: %q", output)
	}

	if !strings.Contains(output, `"recreateRequired"`) {
		t.Errorf("expected JSON output to contain recreateRequired key, got: %q", output)
	}

	if !strings.Contains(output, `"requiresConfirmation"`) {
		t.Errorf("expected JSON output to contain requiresConfirmation key, got: %q", output)
	}

	if strings.Contains(output, "Component") {
		t.Error("JSON output should not contain table headers like 'Component'")
	}

	if strings.Contains(output, "🟢") || strings.Contains(output, "🔴") {
		t.Error("JSON output should not contain emoji icons")
	}
}

func TestDisplayChangesSummary_JSONOutput_NoChanges(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}

	var buf bytes.Buffer

	cmd.SetOut(&buf)

	cmd.Flags().String("output", "json", "")

	diff := clusterupdate.NewEmptyUpdateResult()
	cluster.ExportDisplayChangesSummary(cmd, diff)

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty diff, got %q", buf.String())
	}
}

func TestDiffToJSON_Structure(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		OldValue: "Default",
		NewValue: "Cilium",
		Category: clusterupdate.ChangeCategoryInPlace,
		Reason:   "CNI can be switched",
	})
	diff.RebootRequired = append(diff.RebootRequired, clusterupdate.Change{
		Field:    "talos.kernel",
		OldValue: "",
		NewValue: "console=ttyS0",
		Category: clusterupdate.ChangeCategoryRebootRequired,
		Reason:   "kernel arg change needs reboot",
	})
	diff.RecreateRequired = append(diff.RecreateRequired, clusterupdate.Change{
		Field:    "cluster.distribution",
		OldValue: "Vanilla",
		NewValue: "Talos",
		Category: clusterupdate.ChangeCategoryRecreateRequired,
		Reason:   "distribution change",
	})

	out := cluster.ExportDiffToJSON(diff)

	assertDiffCounts(t, out)
	assertInPlaceChange(t, out.InPlaceChanges[0])
	assertCategories(t, out)
}

func assertDiffCounts(t *testing.T, out cluster.DiffJSONOutput) {
	t.Helper()

	if out.TotalChanges != 3 {
		t.Errorf("expected TotalChanges=3, got %d", out.TotalChanges)
	}

	if len(out.InPlaceChanges) != 1 {
		t.Errorf("expected 1 in-place change, got %d", len(out.InPlaceChanges))
	}

	if len(out.RebootRequired) != 1 {
		t.Errorf("expected 1 reboot-required change, got %d", len(out.RebootRequired))
	}

	if len(out.RecreateRequired) != 1 {
		t.Errorf("expected 1 recreate-required change, got %d", len(out.RecreateRequired))
	}

	if !out.RequiresConfirmation {
		t.Error("expected RequiresConfirmation=true when reboot or recreate changes present")
	}
}

func assertInPlaceChange(t *testing.T, inPlace cluster.ChangeJSON) {
	t.Helper()

	if inPlace.Field != "cluster.cni" {
		t.Errorf("expected field=cluster.cni, got %q", inPlace.Field)
	}

	if inPlace.OldValue != "Default" {
		t.Errorf("expected oldValue=Default, got %q", inPlace.OldValue)
	}

	if inPlace.NewValue != "Cilium" {
		t.Errorf("expected newValue=Cilium, got %q", inPlace.NewValue)
	}

	if inPlace.Category != "in-place" {
		t.Errorf("expected category=in-place, got %q", inPlace.Category)
	}
}

func assertCategories(t *testing.T, out cluster.DiffJSONOutput) {
	t.Helper()

	if out.RecreateRequired[0].Category != "recreate-required" {
		t.Errorf("expected category=recreate-required, got %q", out.RecreateRequired[0].Category)
	}

	if out.RebootRequired[0].Category != "reboot-required" {
		t.Errorf("expected category=reboot-required, got %q", out.RebootRequired[0].Category)
	}
}

func TestDiffToJSON_RequiresConfirmation_OnlyInPlace(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = append(diff.InPlaceChanges, clusterupdate.Change{
		Field:    "cluster.cni",
		Category: clusterupdate.ChangeCategoryInPlace,
	})

	out := cluster.ExportDiffToJSON(diff)

	if out.RequiresConfirmation {
		t.Error("expected RequiresConfirmation=false for in-place-only changes")
	}
}

// TestEnsureLocalRegistriesReady_CloudProviders verifies that Docker infrastructure
// (local registry container, mirror registry containers, Docker network) is skipped
// for cloud providers (Omni, Hetzner). Cloud providers run nodes on remote servers
// and cannot access local Docker infrastructure.
func TestEnsureLocalRegistriesReady_CloudProviders(t *testing.T) {
	t.Parallel()

	cloudProviders := []v1alpha1.Provider{
		v1alpha1.ProviderOmni,
		v1alpha1.ProviderHetzner,
	}

	for _, provider := range cloudProviders {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()

			// Set up a docker invoker that errors on any call.
			// If Docker infra stages are incorrectly executed for cloud providers,
			// this invoker will cause the test to fail.
			errDockerInvoker := func(_ *cobra.Command, _ func(client.APIClient) error) error {
				t.Errorf("Docker should not be called for cloud provider %s", provider)

				return nil
			}

			localDeps := localregistry.NewDependencies(
				localregistry.WithDockerInvoker(errDockerInvoker),
			)

			cmd := &cobra.Command{Use: "test"}
			cmd.Flags().StringSlice("mirror-registry", []string{}, "")
			cmd.SetContext(context.Background())

			ctx := &localregistry.Context{
				ClusterCfg: &v1alpha1.Cluster{
					Spec: v1alpha1.Spec{
						Cluster: v1alpha1.ClusterSpec{
							Distribution: v1alpha1.DistributionTalos,
							Provider:     provider,
						},
					},
				},
			}

			v := viper.New()
			cfgManager := &ksailconfigmanager.ConfigManager{Viper: v}

			deps := lifecycle.Deps{Timer: timer.New()}

			err := cluster.ExportEnsureLocalRegistriesReady(
				cmd,
				ctx,
				deps,
				cfgManager,
				localDeps,
			)
			require.NoError(
				t,
				err,
				"ensureLocalRegistriesReady should succeed for cloud provider %s without Docker",
				provider,
			)
		})
	}
}

func TestComponentLabel_Empty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "(none)", cluster.ExportComponentLabel(""))
}

func TestComponentLabel_None(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "(none)", cluster.ExportComponentLabel("None"))
}

func TestComponentLabel_Disabled(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "(disabled)", cluster.ExportComponentLabel("Disabled"))
}

func TestComponentLabel_ActiveValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Cilium", cluster.ExportComponentLabel("Cilium"))
}

func TestToTalosClusters_Empty(t *testing.T) {
	t.Parallel()

	result := cluster.ExportToTalosClusters(nil)
	assert.Empty(t, result)
}

func TestToTalosClusters_MultipleNames(t *testing.T) {
	t.Parallel()

	names := []string{"cluster-a", "cluster-b"}
	result := cluster.ExportToTalosClusters(names)

	require.Len(t, result, 2)
	assert.Equal(t, "cluster-a", result[0].Name)
	assert.Equal(t, v1alpha1.DistributionTalos, result[0].Distribution)
	assert.Equal(t, "cluster-b", result[1].Name)
	assert.Equal(t, v1alpha1.DistributionTalos, result[1].Distribution)
}

func TestDisplayClusterIdentity_AllFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	info := &clusterdetector.Info{
		ClusterName:    "my-cluster",
		Distribution:   v1alpha1.DistributionVanilla,
		Provider:       v1alpha1.ProviderDocker,
		Context:        "kind-my-cluster",
		ServerURL:      "https://127.0.0.1:6443",
		KubeconfigPath: "/home/user/.kube/config",
	}

	cluster.ExportDisplayClusterIdentity(&buf, info)

	out := buf.String()
	assert.Contains(t, out, "KSail Cluster Details:")
	assert.Contains(t, out, "my-cluster")
	assert.Contains(t, out, string(v1alpha1.DistributionVanilla))
	assert.Contains(t, out, string(v1alpha1.ProviderDocker))
	assert.Contains(t, out, "kind-my-cluster")
	assert.Contains(t, out, "https://127.0.0.1:6443")
	assert.Contains(t, out, "/home/user/.kube/config")
}

func TestDisplayClusterIdentity_OptionalFieldsOmitted(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	info := &clusterdetector.Info{
		ClusterName:  "bare-cluster",
		Distribution: v1alpha1.DistributionVanilla,
		Provider:     v1alpha1.ProviderDocker,
	}

	cluster.ExportDisplayClusterIdentity(&buf, info)

	out := buf.String()
	assert.Contains(t, out, "bare-cluster")
	assert.NotContains(t, out, "Context:")
	assert.NotContains(t, out, "Server:")
	assert.NotContains(t, out, "Kubeconfig:")
}

func TestDisplayTTLInfo_NoTTL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// Using a nonexistent cluster name: no state file → silent return.
	cluster.ExportDisplayTTLInfo(&buf, "nonexistent-cluster-ttl-test")
	assert.Empty(t, buf.String())
}

func TestDisplayTTLInfo_WithActiveTTL(t *testing.T) {
	t.Parallel()

	clusterName := "display-ttl-active-test"

	err := state.SaveClusterTTL(clusterName, 2*time.Hour)
	require.NoError(t, err)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	var buf bytes.Buffer

	cluster.ExportDisplayTTLInfo(&buf, clusterName)

	out := buf.String()
	assert.Contains(t, out, "cluster TTL")
	assert.Contains(t, out, "remaining")
}

func TestDisplayTTLInfo_WithExpiredTTL(t *testing.T) {
	t.Parallel()

	clusterName := "display-ttl-expired-test"

	// Save a TTL of 1ms so it is already expired by the time we read it.
	err := state.SaveClusterTTL(clusterName, time.Millisecond)
	require.NoError(t, err)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	// Wait long enough for the TTL to lapse.
	time.Sleep(10 * time.Millisecond)

	var buf bytes.Buffer

	cluster.ExportDisplayTTLInfo(&buf, clusterName)

	out := buf.String()
	assert.Contains(t, out, "EXPIRED")
}

func TestDisplayComponents_NoState(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// No saved spec → displayComponents returns silently.
	cluster.ExportDisplayComponents(&buf, "nonexistent-cluster-components-test")
	assert.Empty(t, buf.String())
}

func TestDisplayComponents_WithState(t *testing.T) {
	t.Parallel()

	clusterName := "display-components-test"

	spec := &v1alpha1.ClusterSpec{
		GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
		CNI:           v1alpha1.CNICilium,
		CSI:           v1alpha1.CSIDisabled,
		MetricsServer: v1alpha1.MetricsServerDisabled,
		LoadBalancer:  v1alpha1.LoadBalancerDisabled,
		CertManager:   v1alpha1.CertManagerDisabled,
		PolicyEngine:  v1alpha1.PolicyEngineNone,
	}

	err := state.SaveClusterSpec(clusterName, spec)
	require.NoError(t, err)

	t.Cleanup(func() { _ = state.DeleteClusterState(clusterName) })

	var buf bytes.Buffer

	cluster.ExportDisplayComponents(&buf, clusterName)

	out := buf.String()
	assert.Contains(t, out, "Components:")
	assert.Contains(t, out, string(v1alpha1.GitOpsEngineFlux))
	assert.Contains(t, out, string(v1alpha1.CNICilium))
	assert.Contains(t, out, "(disabled)")
	assert.Contains(t, out, "(none)")
}

// TestClassifyRestoreError_FallbackToErrMsg verifies that classifyRestoreError
// falls back to err.Error() when stderr is empty, so "already exists" errors
// routed through BehaviorOnFatal (not stderr) are correctly suppressed.
//
//nolint:funlen,err113 // Table-driven test with comprehensive cases; test errors are intentionally dynamic
func TestClassifyRestoreError_FallbackToErrMsg(t *testing.T) {
	t.Parallel()

	// Sentinel errors used as test inputs for classifyRestoreError.
	var (
		errExitStatus1     = errors.New("exit status 1")
		errDaemonSetExists = errors.New(
			"Error from server (AlreadyExists): daemonsets.apps \"svclb-traefik\" already exists",
		)
		errMultipleAlreadyExist = errors.New(
			"daemonsets.apps \"svclb-traefik\" already exists\n" +
				"jobs.batch \"helm-install-traefik\" already exists",
		)
		errMixedExistsAndOther = errors.New(
			"daemonsets.apps \"svclb-traefik\" already exists\n" +
				"connection refused",
		)
		errConnectionRefused = errors.New("connection refused")
		errAlreadyExists     = errors.New("already exists")
	)

	tests := []struct {
		name      string
		err       error
		stderr    string
		policy    string
		expectNil bool
	}{
		{
			name:      "nil error returns nil",
			err:       nil,
			stderr:    "",
			policy:    "none",
			expectNil: true,
		},
		{
			name:      "already exists in stderr with policy none",
			err:       errExitStatus1,
			stderr:    "Error from server (AlreadyExists): resource already exists",
			policy:    "none",
			expectNil: true,
		},
		{
			name:      "already exists in err.Error() with empty stderr",
			err:       errDaemonSetExists,
			stderr:    "",
			policy:    "none",
			expectNil: true,
		},
		{
			name:      "already exists in err.Error() with whitespace-only stderr",
			err:       errDaemonSetExists,
			stderr:    "\n",
			policy:    "none",
			expectNil: true,
		},
		{
			name:      "multiple already exists lines in err.Error()",
			err:       errMultipleAlreadyExist,
			stderr:    "",
			policy:    "none",
			expectNil: true,
		},
		{
			name:      "mixed error in err.Error() with empty stderr",
			err:       errMixedExistsAndOther,
			stderr:    "",
			policy:    "none",
			expectNil: false,
		},
		{
			name:      "real error with empty stderr",
			err:       errConnectionRefused,
			stderr:    "",
			policy:    "none",
			expectNil: false,
		},
		{
			name:      "already exists with policy update does not suppress",
			err:       errAlreadyExists,
			stderr:    "",
			policy:    "update",
			expectNil: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := cluster.ExportClassifyRestoreError(
				testCase.err, testCase.stderr, testCase.policy,
			)
			if testCase.expectNil {
				assert.NoError(t, result)
			} else {
				assert.Error(t, result)
			}
		})
	}
}

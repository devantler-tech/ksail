package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBackupMetadata(t *testing.T) {
	t.Parallel()

	metadata := &backup.BackupMetadata{
		Version:       "v1",
		ClusterName:   "test-cluster",
		Distribution:  "Vanilla",
		Provider:      tProviderDocker,
		KSailVersion:  "5.0.0",
		ResourceCount: 42,
		ResourceTypes: []string{tTypeDeploys, tTypeServices},
	}

	tmpDir := t.TempDir()
	metadataPath := filepath.Join(tmpDir, "backup-metadata.json")

	err := backup.WriteMetadata(metadata, metadataPath)
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

	err := os.WriteFile(testFile, []byte("test content"), backup.FilePerm)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	subDir := filepath.Join(srcDir, "subdir")

	err = os.MkdirAll(subDir, backup.DirPerm)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	subFile := filepath.Join(subDir, "sub.txt")

	err = os.WriteFile(subFile, []byte("sub content"), backup.FilePerm)
	if err != nil {
		t.Fatalf("failed to create sub file: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "test-backup.tar.gz")

	err = backup.CreateTarball(srcDir, outputPath, 6)
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

			count := backup.CountYAMLDocuments(test.content)
			if count != test.expected {
				t.Errorf(
					"backup.CountYAMLDocuments() = %d, want %d",
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
			types:       []string{tTypePods, tTypeServices, tTypeDeploys},
			exclude:     []string{},
			expectedLen: 3,
		},
		{
			name:        "exclude one",
			types:       []string{tTypePods, tTypeServices, tTypeDeploys},
			exclude:     []string{tTypePods},
			expectedLen: 2,
		},
		{
			name:        "exclude all",
			types:       []string{tTypePods},
			exclude:     []string{tTypePods},
			expectedLen: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := backup.FilterExcludedTypes(test.types, test.exclude)
			if len(result) != test.expectedLen {
				t.Errorf(
					"backup.FilterExcludedTypes() returned %d items, want %d",
					len(result), test.expectedLen,
				)
			}
		})
	}
}

func TestExtractAndReadMetadata(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	metadata := &backup.BackupMetadata{
		Version:       "v1",
		ClusterName:   "roundtrip-cluster",
		Distribution:  "K3s",
		Provider:      tProviderDocker,
		KSailVersion:  "5.0.0",
		ResourceCount: 10,
		ResourceTypes: []string{tTypeDeploys, tTypeServices},
	}

	metadataPath := filepath.Join(srcDir, "backup-metadata.json")

	err := backup.WriteMetadata(metadata, metadataPath)
	if err != nil {
		t.Fatalf("failed to write metadata: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "test.tar.gz")

	err = backup.CreateTarball(srcDir, archivePath, 6)
	if err != nil {
		t.Fatalf("failed to create tarball: %v", err)
	}

	tmpDir, restored, err := backup.ExtractBackupArchive(archivePath)
	if err != nil {
		t.Fatalf("failed to extract backup archive: %v", err)
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	assertMetadataRoundtrip(t, restored)
}

func assertMetadataRoundtrip(t *testing.T, restored *backup.BackupMetadata) {
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

	if restored.Provider != tProviderDocker {
		t.Errorf("Provider = %q, want %q", restored.Provider, tProviderDocker)
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

	result, err := backup.SanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("backup.SanitizeYAMLOutput() error = %v", err)
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

	result, err := backup.SanitizeYAMLOutput("not valid yaml: [")
	if err != nil {
		t.Fatalf("backup.SanitizeYAMLOutput() error = %v", err)
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

	result, err := backup.SanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("backup.SanitizeYAMLOutput() error = %v", err)
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

	result, err := backup.SanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("backup.SanitizeYAMLOutput() error = %v", err)
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

	result, err := backup.SanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("backup.SanitizeYAMLOutput() error = %v", err)
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

	result, err := backup.SanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("backup.SanitizeYAMLOutput() error = %v", err)
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

	result, err := backup.SanitizeYAMLOutput(input)
	if err != nil {
		t.Fatalf("backup.SanitizeYAMLOutput() error = %v", err)
	}

	if !strings.Contains(result, tClusterIP) {
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
			kind:     tKindSecret,
			objType:  "helm.sh/release.v1",
			expected: true,
		},
		{
			name:     "opaque secret",
			kind:     tKindSecret,
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
			kind:     tKindSecret,
			objType:  "",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion":   "v1",
					"kind":         test.kind,
					tMetadataField: map[string]any{"name": "test"},
				},
			}
			if test.objType != "" {
				obj.Object[tTypeField] = test.objType
			}

			result := backup.IsHelmReleaseSecret(obj)
			if result != test.expected {
				t.Errorf(
					"backup.IsHelmReleaseSecret() = %v, want %v",
					result, test.expected,
				)
			}
		})
	}
}

func TestBackupResourceTypes(t *testing.T) {
	t.Parallel()

	types := backup.BackupResourceTypes()

	require.NotEmpty(t, types, "backupResourceTypes must return a non-empty list")

	// Validate that the list contains well-known resource types.
	typeSet := make(map[string]struct{}, len(types))
	for _, rt := range types {
		typeSet[rt] = struct{}{}
	}

	for _, expected := range []string{
		tTypeCRDs,
		tTypeNamespaces,
		tTypeDeploys,
	} {
		assert.Contains(t, typeSet, expected,
			"backupResourceTypes should include %q", expected)
	}

	// CRDs must come before namespaces in restore ordering.
	crdIdx, nsIdx := -1, -1

	for i, rt := range types {
		switch rt {
		case tTypeCRDs:
			crdIdx = i
		case tTypeNamespaces:
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

func TestFilterExcludedTypes_DuplicateExclusions(t *testing.T) {
	t.Parallel()

	got := backup.FilterExcludedTypes(
		[]string{tTypePods, tTypeServices, tTypeDeploys},
		[]string{tTypePods, tTypePods},
	)
	assert.Equal(t, []string{tTypeServices, tTypeDeploys}, got)
}

func TestFilterExcludedTypes_PreservesOrder(t *testing.T) {
	t.Parallel()

	got := backup.FilterExcludedTypes(
		[]string{"z", "a", "m", "b"},
		[]string{"m"},
	)
	assert.Equal(t, []string{"z", "a", "b"}, got)
}

func TestCountYAMLDocuments_MixedListAndKind(t *testing.T) {
	t.Parallel()

	content := "- apiVersion: v1\n  kind: Pod\nkind: Service"
	got := backup.CountYAMLDocuments(content)
	assert.Equal(t, 2, got)
}

func TestClusterScopedResourceTypes(t *testing.T) {
	t.Parallel()

	types := backup.ClusterScopedResourceTypes()
	assert.True(t, types[tTypeCRDs])
	assert.True(t, types[tTypeNamespaces])
	assert.True(t, types["storageclasses"])
	assert.True(t, types["persistentvolumes"])
	assert.True(t, types["clusterroles"])
	assert.True(t, types["clusterrolebindings"])
	assert.False(t, types[tTypePods])
	assert.False(t, types[tTypeDeploys])
	assert.Len(t, types, 6)
}

func TestRemoveAutoGeneratedJobLabels( //nolint:funlen // Table-driven tests are naturally long.
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name          string
		labels        map[string]string
		expectLabels  map[string]string
		expectRemoved bool
	}{
		{
			name: "removes batch controller-uid",
			labels: map[string]string{
				tBatchUIDLabel: tUIDValue,
				tLabelApp:      tLabelValueApp,
			},
			expectLabels: map[string]string{
				tLabelApp: tLabelValueApp,
			},
		},
		{
			name: "removes controller-uid",
			labels: map[string]string{
				tControllerUID: tUIDValue,
				tLabelApp:      tLabelValueApp,
			},
			expectLabels: map[string]string{
				tLabelApp: tLabelValueApp,
			},
		},
		{
			name: "removes both auto-generated labels",
			labels: map[string]string{
				tBatchUIDLabel: tUIDValue,
				tControllerUID: "def456",
				tLabelApp:      tLabelValueApp,
			},
			expectLabels: map[string]string{
				tLabelApp: tLabelValueApp,
			},
		},
		{
			name: "removes entire labels field when empty after cleanup",
			labels: map[string]string{
				tBatchUIDLabel: tUIDValue,
				tControllerUID: "def456",
			},
			expectRemoved: true,
		},
		{
			name:         "no labels to remove",
			labels:       map[string]string{tLabelApp: tLabelValueApp},
			expectLabels: map[string]string{tLabelApp: tLabelValueApp},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{Object: map[string]any{
				tMetadataField: map[string]any{
					"labels": convertLabels(testCase.labels),
				},
			}}

			backup.RemoveAutoGeneratedJobLabels(obj, tMetadataField, "labels")

			if testCase.expectRemoved {
				labels, found, _ := unstructured.NestedStringMap(
					obj.Object,
					tMetadataField,
					"labels",
				)
				assert.True(t, !found || len(labels) == 0)
			} else if testCase.expectLabels != nil {
				labels, _, _ := unstructured.NestedStringMap(obj.Object, tMetadataField, "labels")
				assert.Equal(t, testCase.expectLabels, labels)
			}
		})
	}
}

func convertLabels(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}

	return result
}

func TestRemoveAutoGeneratedJobLabels_NoLabels(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{Object: map[string]any{
		tMetadataField: map[string]any{},
	}}

	// Should not panic
	backup.RemoveAutoGeneratedJobLabels(obj, tMetadataField, "labels")
}

func TestRemoveServiceClusterIPs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		spec       map[string]any
		expectSpec map[string]any
	}{
		{
			name: "removes clusterIP and clusterIPs",
			spec: map[string]any{
				tClusterIP:  "10.0.0.1",
				tClusterIPs: []any{"10.0.0.1"},
				tTypeField:  tClusterIPType,
			},
			expectSpec: map[string]any{
				tTypeField: tClusterIPType,
			},
		},
		{
			name: "preserves headless service",
			spec: map[string]any{
				tClusterIP:  tClusterIPNone,
				tClusterIPs: []any{tClusterIPNone},
				tTypeField:  tClusterIPType,
			},
			expectSpec: map[string]any{
				tClusterIP:  tClusterIPNone,
				tClusterIPs: []any{tClusterIPNone},
				tTypeField:  tClusterIPType,
			},
		},
		{
			name: "no clusterIP field",
			spec: map[string]any{
				tTypeField: tClusterIPType,
			},
			expectSpec: map[string]any{
				tTypeField: tClusterIPType,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{Object: map[string]any{
				"spec": testCase.spec,
			}}

			backup.RemoveServiceClusterIPs(obj)

			spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
			assert.Equal(t, testCase.expectSpec, spec)
		})
	}
}

func TestCreateTarball_OverwritesExistingTarget(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()

	err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("data"), backup.FilePerm)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	outputPath := filepath.Join(t.TempDir(), "backup.tar.gz")

	// First run creates the archive.
	err = backup.CreateTarball(srcDir, outputPath, -1)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run to the same path must succeed (overwrite) without error.
	err = backup.CreateTarball(srcDir, outputPath, -1)
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

	err := backup.CreateTarball(nonexistentDir, outputPath, -1)
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

	err := os.WriteFile(realFile, []byte("data"), backup.FilePerm)
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

	err = backup.CreateTarball(srcDir, outputPath, -1)
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

func TestDirFilePerm(t *testing.T) {
	t.Parallel()
	assert.Equal(t, os.FileMode(0o750), os.FileMode(backup.DirPerm))
	assert.Equal(t, os.FileMode(0o600), os.FileMode(backup.FilePerm))
}

func TestCommitTarball(t *testing.T) {
	t.Parallel()

	t.Run("successful commit", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tmpPath := filepath.Join(tmpDir, "temp.tar.gz")
		targetPath := filepath.Join(tmpDir, "final.tar.gz")

		//nolint:gosec // Test creates a file inside its own temp directory.
		outFile, err := os.Create(tmpPath)
		require.NoError(t, err)

		gzipWriter := gzip.NewWriter(outFile)
		tarWriter := tar.NewWriter(gzipWriter)

		err = backup.CommitTarball(tarWriter, gzipWriter, outFile, tmpPath, targetPath)
		require.NoError(t, err)

		_, err = os.Stat(targetPath)
		require.NoError(t, err)

		_, err = os.Stat(tmpPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("overwrites existing target", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		tmpPath := filepath.Join(tmpDir, "temp.tar.gz")
		targetPath := filepath.Join(tmpDir, "final.tar.gz")

		// Create existing target
		require.NoError(t, os.WriteFile(targetPath, []byte("old"), 0o600))

		//nolint:gosec // Test creates a file inside its own temp directory.
		outFile, err := os.Create(tmpPath)
		require.NoError(t, err)

		gzipWriter := gzip.NewWriter(outFile)
		tarWriter := tar.NewWriter(gzipWriter)

		err = backup.CommitTarball(tarWriter, gzipWriter, outFile, tmpPath, targetPath)
		require.NoError(t, err)

		_, err = os.Stat(targetPath)
		assert.NoError(t, err)
	})
}

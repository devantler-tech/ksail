package yamlgenerator_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

const testFilePermissions = 0o600

type generatorWithGenerate[T any] interface {
	Generate(model T, options yamlgenerator.Options) (string, error)
}

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func assertFileEquals(t *testing.T, dir string, path string, expected string) {
	t.Helper()

	// Ensure we read the file via an absolute path rooted at dir.
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}

	//nolint:gosec // G304: path is created by the test (temp directory).
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, expected, string(content))
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	gen := yamlgenerator.NewGenerator[map[string]any]()

	createCluster := func(name string) map[string]any {
		return map[string]any{"name": name}
	}

	assertContent := func(t *testing.T, result, _ string) {
		t.Helper()
		snaps.MatchSnapshot(t, result)
	}

	runStandardGeneratorTests(
		t,
		gen,
		createCluster,
		"output.yaml",
		assertContent,
	)
}

type standardGeneratorTestCase struct {
	name        string
	clusterName string
	force       bool
	setupOutput func(*testing.T) (outputPath string, verifyFile bool, tempDir string)
}

func standardGeneratorTestCases(expectedFileName string) []standardGeneratorTestCase {
	return []standardGeneratorTestCase{
		{
			name:        "without file",
			clusterName: "test-cluster",
			setupOutput: func(_ *testing.T) (string, bool, string) { return "", false, "" },
		},
		{
			name:        "with file",
			clusterName: "file-cluster",
			setupOutput: func(t *testing.T) (string, bool, string) {
				t.Helper()
				tempDir := t.TempDir()
				outputPath := filepath.Join(tempDir, expectedFileName)

				return outputPath, true, tempDir
			},
		},
		{
			name:        "with force overwrite",
			clusterName: "force-cluster",
			force:       true,
			setupOutput: func(t *testing.T) (string, bool, string) {
				t.Helper()
				tempDir := t.TempDir()
				outputPath := filepath.Join(tempDir, expectedFileName)

				err := os.WriteFile(outputPath, []byte("existing content"), testFilePermissions)
				require.NoError(t, err)

				return outputPath, true, tempDir
			},
		},
	}
}

func runStandardGeneratorTests[T any](
	t *testing.T,
	gen generatorWithGenerate[T],
	createCluster func(name string) T,
	expectedFileName string,
	assertContent func(*testing.T, string, string),
) {
	t.Helper()

	testCases := standardGeneratorTestCases(expectedFileName)
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := createCluster(testCase.clusterName)
			output, verifyFile, tempDir := testCase.setupOutput(t)
			opts := yamlgenerator.Options{Output: output, Force: testCase.force}

			result, err := gen.Generate(cluster, opts)
			require.NoError(t, err)
			assertContent(t, result, testCase.clusterName)

			if verifyFile {
				assertFileEquals(t, tempDir, filepath.Join(tempDir, expectedFileName), result)
			}
		})
	}
}

func TestGenerateWithComplexModel(t *testing.T) {
	t.Parallel()

	gen := yamlgenerator.NewGenerator[map[string]any]()

	// Test with complex nested structure
	complexModel := map[string]any{
		"metadata": map[string]any{
			"name":      "test-cluster",
			"namespace": "default",
			"labels": map[string]string{
				"app": "ksail",
				"env": "test",
			},
		},
		"spec": map[string]any{
			"replicas": 3,
			"ports":    []int{8080, 9090},
			"config": map[string]any{
				"enabled": true,
				"timeout": "30s",
			},
		},
	}

	result, err := gen.Generate(complexModel, yamlgenerator.Options{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Verify YAML structure is valid by checking it contains expected keys
	expectedKeys := []string{"metadata:", "spec:", "name:", "replicas:"}
	for _, key := range expectedKeys {
		if !strings.Contains(result, key) {
			t.Errorf("expected result to contain %q, but it didn't. Result: %s", key, result)
		}
	}
}

func TestGenerateWithEmptyModel(t *testing.T) {
	t.Parallel()

	gen := yamlgenerator.NewGenerator[map[string]any]()

	// Test with empty model
	emptyModel := map[string]any{}

	result, err := gen.Generate(emptyModel, yamlgenerator.Options{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expected := "{}\n"
	if result != expected {
		t.Fatalf("expected result %q, got %q", expected, result)
	}
}

func TestGenerateWithOutputPath(t *testing.T) {
	t.Parallel()

	gen := yamlgenerator.NewGenerator[map[string]any]()
	tempDir := t.TempDir()

	model := map[string]any{
		"test": "value",
	}

	outputPath := tempDir + "/test-output.yaml"

	result, err := gen.Generate(model, yamlgenerator.Options{
		Output: outputPath,
		Force:  false,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result when writing to file")
	}

	// Verify the result contains our test data
	if !strings.Contains(result, "test: value") {
		t.Errorf("expected result to contain 'test: value', got: %s", result)
	}
}

func TestGenerateWithInvalidOutputDirectory(t *testing.T) {
	t.Parallel()

	gen := yamlgenerator.NewGenerator[map[string]any]()

	model := map[string]any{
		"test": "value",
	}

	// Use invalid path that should cause write error
	invalidPath := "/invalid/path/that/does/not/exist/output.yaml"

	_, err := gen.Generate(model, yamlgenerator.Options{
		Output: invalidPath,
		Force:  false,
	})
	if err == nil {
		t.Fatal("expected error for invalid output path, got none")
	}

	expectedErrorSubstring := "failed to write YAML to file"
	if !strings.Contains(err.Error(), expectedErrorSubstring) {
		t.Errorf("expected error to contain %q, got: %v", expectedErrorSubstring, err)
	}
}

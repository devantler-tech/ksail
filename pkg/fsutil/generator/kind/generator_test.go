package kindgenerator_test

import (
	"os"
	"path/filepath"
	"testing"

	generator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/kind"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const testFilePermissions = 0o600

type generatorWithGenerate[T any] interface {
	Generate(model T, opts yamlgenerator.Options) (string, error)
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

func assertFileEquals(t *testing.T, path string, expected string) {
	t.Helper()

	//nolint:gosec // G304: path is created by the test (temp directory).
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, expected, string(content))
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	gen := generator.NewGenerator()

	createCluster := func(_ string) *kindv1alpha4.Cluster {
		return &kindv1alpha4.Cluster{
			TypeMeta: kindv1alpha4.TypeMeta{
				APIVersion: "kind.x-k8s.io/v1alpha4",
				Kind:       "Cluster",
			},
		}
	}

	assertContent := func(t *testing.T, result, _ string) {
		t.Helper()
		snaps.MatchSnapshot(t, result)
	}

	runStandardGeneratorTests(t, gen, createCluster, "kind.yaml", assertContent)
}

type standardGeneratorTestCase struct {
	Name        string
	ClusterName string
	Force       bool
	SetupOutput func(*testing.T) (outputPath string, verifyFile bool, tempDir string)
}

func standardGeneratorTestCases(expectedFileName string) []standardGeneratorTestCase {
	return []standardGeneratorTestCase{
		{
			Name:        "without file",
			ClusterName: "test-cluster",
			SetupOutput: func(_ *testing.T) (string, bool, string) { return "", false, "" },
		},
		{
			Name:        "with file",
			ClusterName: "file-cluster",
			SetupOutput: func(t *testing.T) (string, bool, string) {
				t.Helper()
				tempDir := t.TempDir()
				outputPath := filepath.Join(tempDir, expectedFileName)

				return outputPath, true, tempDir
			},
		},
		{
			Name:        "with force overwrite",
			ClusterName: "force-cluster",
			Force:       true,
			SetupOutput: func(t *testing.T) (string, bool, string) {
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

	for _, testCase := range standardGeneratorTestCases(expectedFileName) {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			cluster := createCluster(testCase.ClusterName)
			output, verifyFile, tempDir := testCase.SetupOutput(t)
			opts := yamlgenerator.Options{Output: output, Force: testCase.Force}

			result, err := gen.Generate(cluster, opts)
			require.NoError(t, err)
			assertContent(t, result, testCase.ClusterName)

			if verifyFile {
				expectedPath := filepath.Join(tempDir, expectedFileName)
				assertFileEquals(t, expectedPath, result)
			}
		})
	}
}

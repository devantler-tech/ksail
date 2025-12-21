package k3dgenerator_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	generator "github.com/devantler-tech/ksail/v5/pkg/io/generator/k3d"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testFilePermissions = 0o600

var (
	errMarshalFail   = errors.New("marshal error")
	errUnmarshalFail = errors.New("unmarshal error")
)

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

type marshalFailer[T any] struct{}

func (m marshalFailer[T]) Marshal(_ T) (string, error) {
	return "", errMarshalFail
}

func (m marshalFailer[T]) Unmarshal(_ []byte, _ *T) error {
	return errUnmarshalFail
}

func (m marshalFailer[T]) UnmarshalString(_ string, _ *T) error {
	return errUnmarshalFail
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	gen := generator.NewK3dGenerator()

	createCluster := func(_ string) *v1alpha5.SimpleConfig {
		return &v1alpha5.SimpleConfig{}
	}

	assertContent := func(t *testing.T, result, _ string) {
		t.Helper()
		snaps.MatchSnapshot(t, result)
	}

	runStandardGeneratorTests(t, gen, createCluster, "k3d.yaml", assertContent)
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

// assertK3dYAMLContains verifies that the generated YAML contains expected k3d structure.
func assertK3dYAMLContains(t *testing.T, result, clusterName string) {
	t.Helper()
	assert.Contains(t, result, "apiVersion: k3d.io/v1alpha5")
	assert.Contains(t, result, "kind: Simple")
	assert.Contains(t, result, "name: "+clusterName)
}

// assertComplexK3dConfig verifies complex configuration fields in generated YAML.
func assertComplexK3dConfig(t *testing.T, result string) {
	t.Helper()
	assert.Contains(t, result, "servers: 3")
	assert.Contains(t, result, "agents: 2")
	assert.Contains(t, result, "image: rancher/k3s:v1.25.0-k3s1")
	assert.Contains(t, result, "network: test-network")
	assert.Contains(t, result, "wait: true")
	assert.Contains(t, result, "disableImageVolume: true")
	assert.Contains(t, result, "updateDefaultKubeconfig: true")
	assert.Contains(t, result, "switchCurrentContext: true")
}

// assertPortMappingConfig verifies port mapping and environment configuration in generated YAML.
func assertPortMappingConfig(t *testing.T, result string) {
	t.Helper()
	assert.Contains(t, result, "port: 8080:80")
	assert.Contains(t, result, "port: 8443:443")
	assert.Contains(t, result, "envVar: MY_VAR=test")
	assert.Contains(t, result, "nodeFilters:")
	assert.Contains(t, result, "- loadbalancer")
	assert.Contains(t, result, "- all")
}

func TestGenerateWithComplexConfig(t *testing.T) {
	t.Parallel()

	gen := generator.NewK3dGenerator()

	cluster := &v1alpha5.SimpleConfig{
		Servers: 3,
		Agents:  2,
		Image:   "rancher/k3s:v1.25.0-k3s1",
		Network: "test-network",
		Options: v1alpha5.SimpleConfigOptions{
			K3dOptions: v1alpha5.SimpleConfigOptionsK3d{
				Wait:                true,
				DisableLoadbalancer: false,
				DisableImageVolume:  true,
			},
			KubeconfigOptions: v1alpha5.SimpleConfigOptionsKubeconfig{
				UpdateDefaultKubeconfig: true,
				SwitchCurrentContext:    true,
			},
		},
	}
	// Set name via ObjectMeta
	cluster.Name = "complex-cluster"

	opts := yamlgenerator.Options{}
	result, err := gen.Generate(cluster, opts)

	require.NoError(t, err)
	assertK3dYAMLContains(t, result, "complex-cluster")
	assertComplexK3dConfig(t, result)
}

func TestGenerateWithPortMappings(t *testing.T) {
	t.Parallel()

	gen := generator.NewK3dGenerator()

	cluster := &v1alpha5.SimpleConfig{
		Ports: []v1alpha5.PortWithNodeFilters{
			{
				Port:        "8080:80",
				NodeFilters: []string{"loadbalancer"},
			},
			{
				Port:        "8443:443",
				NodeFilters: []string{"loadbalancer"},
			},
		},
		Env: []v1alpha5.EnvVarWithNodeFilters{
			{
				EnvVar:      "MY_VAR=test",
				NodeFilters: []string{"all"},
			},
		},
	}
	// Set name via ObjectMeta
	cluster.Name = "port-mapping-cluster"

	opts := yamlgenerator.Options{}
	result, err := gen.Generate(cluster, opts)

	require.NoError(t, err)
	assertK3dYAMLContains(t, result, "port-mapping-cluster")
	assertPortMappingConfig(t, result)
}

func TestGenerateWithFailingMarshaller(t *testing.T) {
	t.Parallel()

	// Create generator with failing marshaller
	gen := &generator.K3dGenerator{
		Marshaller: &marshalFailer[*v1alpha5.SimpleConfig]{},
	}

	cluster := &v1alpha5.SimpleConfig{}
	opts := yamlgenerator.Options{}

	result, err := gen.Generate(cluster, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal k3d config:")
	assert.Empty(t, result)
}

func TestGenerateWithInvalidOutputDirectory(t *testing.T) {
	t.Parallel()

	gen := generator.NewK3dGenerator()
	cluster := &v1alpha5.SimpleConfig{}

	// Use invalid directory path
	opts := yamlgenerator.Options{
		Output: "/nonexistent/directory/k3d.yaml",
	}

	result, err := gen.Generate(cluster, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write k3d config:")
	assert.Empty(t, result)
}

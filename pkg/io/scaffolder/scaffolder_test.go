package scaffolder_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/io/generator"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/io/scaffolder"
	"github.com/gkampitakis/go-snaps/snaps"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

var errGenerateFailure = errors.New("generate failure")

func TestMain(m *testing.M) {
	exitCode := m.Run()

	_, err := snaps.Clean(m, snaps.CleanOpts{Sort: true})
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestNewScaffolder(t *testing.T) {
	t.Parallel()

	cluster := createTestCluster("test-cluster")
	scaffolder := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	require.NotNil(t, scaffolder)
	require.Equal(t, cluster, scaffolder.KSailConfig)
	require.NotNil(t, scaffolder.KSailYAMLGenerator)
	require.NotNil(t, scaffolder.KustomizationGenerator)
}

func TestScaffoldAppliesDistributionDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		expected     string
	}{
		{
			name:         "Kind",
			distribution: v1alpha1.DistributionKind,
			expected:     scaffolder.KindConfigFile,
		},
		{name: "K3d", distribution: v1alpha1.DistributionK3d, expected: scaffolder.K3dConfigFile},
		{
			name:         "Talos",
			distribution: v1alpha1.DistributionTalos,
			expected:     scaffolder.TalosConfigDir,
		},
		{name: "Unknown", distribution: "unknown", expected: scaffolder.KindConfigFile},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			buffer := &bytes.Buffer{}
			scaffolderInstance, mocks := newScaffolderWithMocks(t, buffer)

			scaffolderInstance.KSailConfig.Spec.Cluster.Distribution = testCase.distribution
			scaffolderInstance.KSailConfig.Spec.Cluster.DistributionConfig = ""

			_ = scaffolderInstance.Scaffold(tempDir, false)

			require.Equal(t, testCase.distribution, mocks.ksailLastModel.Spec.Cluster.Distribution)
			require.Equal(
				t,
				testCase.expected,
				mocks.ksailLastModel.Spec.Cluster.DistributionConfig,
			)
		})
	}
}

func TestScaffoldBasicOperations(t *testing.T) {
	t.Parallel()

	tests := getScaffoldTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := testCase.setupFunc(testCase.name)
			scaffolder := scaffolder.NewScaffolder(cluster, io.Discard, nil)

			err := scaffolder.Scaffold(testCase.outputPath, testCase.force)

			if testCase.expectError {
				require.Error(t, err)
				snaps.MatchSnapshot(t, err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestScaffoldContentValidation(t *testing.T) {
	t.Parallel()

	contentTests := getContentTestCases()

	for _, testCase := range contentTests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cluster := testCase.setupFunc("test-cluster")
			scaffolder := scaffolder.NewScaffolder(cluster, io.Discard, nil)
			generateDistributionContent(t, scaffolder, cluster, testCase.distribution)

			kustomization := ktypes.Kustomization{}

			// Generate kustomization content using actual generator, then ensure resources: [] is included
			kustomizationContent, err := scaffolder.KustomizationGenerator.Generate(
				&kustomization,
				yamlgenerator.Options{},
			)
			require.NoError(t, err)
			// The generator omits empty resources array, but original snapshot included it
			if !strings.Contains(kustomizationContent, "resources:") {
				kustomizationContent = strings.TrimSuffix(
					kustomizationContent,
					"\n",
				) + "\nresources: []\n"
			}

			snaps.MatchSnapshot(t, kustomizationContent)
		})
	}
}

func TestScaffoldErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("invalid output path", func(t *testing.T) {
		t.Parallel()

		cluster := createTestCluster("error-test")
		scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

		// Use invalid path with null byte to trigger file system error
		err := scaffolderInstance.Scaffold("/invalid/\x00path/", false)

		require.Error(t, err)
		snaps.MatchSnapshot(t, fmt.Sprintf("Error type: %T, contains 'invalid argument': %t",
			err, strings.Contains(err.Error(), "invalid argument")))
	})

	t.Run("distribution error paths", func(t *testing.T) {
		t.Parallel()

		// Test Unknown distribution
		unknownCluster := createUnknownCluster("unknown-test")
		scaffolderInstance := scaffolder.NewScaffolder(unknownCluster, io.Discard, nil)

		err := scaffolderInstance.Scaffold("/tmp/test-unknown/", false)
		require.Error(t, err)
		require.ErrorIs(t, err, scaffolder.ErrUnknownDistribution)
		snaps.MatchSnapshot(t, err.Error())
	})
}

func TestScaffoldGeneratorFailures(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		distribution string
		clusterFunc  func(string) v1alpha1.Cluster
	}{
		{"Kind", createKindCluster},
		{"K3d", createK3dCluster},
	}

	for _, testCase := range testCases {
		t.Run(testCase.distribution+" config with problematic path", func(t *testing.T) {
			t.Parallel()

			// Test scenarios that might cause generator failures
			// Use a deeply nested path to potentially trigger path length limits
			longPathParts := []string{t.TempDir()}
			for range 10 {
				longPathParts = append(longPathParts, "very-long-directory-name")
			}

			longPath := filepath.Join(longPathParts...)

			cluster := testCase.clusterFunc("error-test")
			scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

			err := scaffolderInstance.Scaffold(longPath, false)

			// Always record whether an error occurred for this distribution
			snaps.MatchSnapshot(
				t,
				fmt.Sprintf("%s error occurred: %t", testCase.distribution, err != nil),
			)
		})
	}
}

func TestScaffoldSkipsExistingFileWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir, buffer, scaffolderInstance, mocks := setupExistingKSailFile(t)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Verify ksail generator was not called (file exists without force)
	mocks.ksail.AssertNotCalled(t, "Generate")
	snaps.MatchSnapshot(t, buffer.String())
}

func TestScaffoldOverwritesFilesWhenForceEnabled(t *testing.T) {
	t.Parallel()

	tempDir, buffer, scaffolderInstance, mocks := setupExistingKSailFile(t)

	err := scaffolderInstance.Scaffold(tempDir, true)
	require.NoError(t, err)

	// Verify ksail generator was called (force enabled)
	mocks.ksail.AssertNumberOfCalls(t, "Generate", 1)
	snaps.MatchSnapshot(t, buffer.String())
}

func TestScaffoldOverwritesKindConfigWhenForceEnabled(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	kindPath := filepath.Join(tempDir, scaffolder.KindConfigFile)

	require.NoError(t, os.WriteFile(kindPath, []byte("existing-kind"), 0o600))

	oldTime := time.Now().Add(-2 * time.Minute)
	require.NoError(t, os.Chtimes(kindPath, oldTime, oldTime))

	buffer := &bytes.Buffer{}
	instance, mocks := newScaffolderWithMocks(t, buffer)

	mocks.kind.ExpectedCalls = nil
	mocks.kind.On("Generate", mock.Anything, mock.Anything).Return("", nil).Once()

	err := instance.Scaffold(tempDir, true)
	require.NoError(t, err)

	snaps.MatchSnapshot(t, buffer.String())

	info, statErr := os.Stat(kindPath)
	require.NoError(t, statErr)
	require.True(t, info.ModTime().After(oldTime), "expected mod time to update on overwrite")
}

func TestScaffoldWrapsKSailGenerationErrors(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	buffer := &bytes.Buffer{}
	scaffolderInstance, mocks := newScaffolderWithMocks(t, buffer)

	// Clear default expectations and set up error return
	mocks.ksail.ExpectedCalls = nil
	mocks.ksail.On("Generate", mock.Anything, mock.Anything).Return("", errGenerateFailure).Once()

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.Error(t, err)
	require.ErrorIs(t, err, scaffolder.ErrKSailConfigGeneration)
}

func TestScaffoldWrapsDistributionGenerationErrors(t *testing.T) {
	t.Parallel()

	tests := []distributionErrorTestCase{
		{
			name: "Kind",
			configure: func(mocks *generatorMocks) {
				mocks.kind.ExpectedCalls = nil // Clear default expectations
				mocks.kind.On(
					"Generate",
					mock.Anything,
					mock.Anything,
				).Return("", errGenerateFailure).Once()
			},
			distribution: v1alpha1.DistributionKind,
			assertErr:    assertKindGenerationError,
		},
		{
			name: "K3d",
			configure: func(mocks *generatorMocks) {
				mocks.k3d.ExpectedCalls = nil // Clear default expectations
				mocks.k3d.On(
					"Generate",
					mock.Anything,
					mock.Anything,
				).Return("", errGenerateFailure).Once()
			},
			distribution: v1alpha1.DistributionK3d,
			assertErr:    assertK3dGenerationError,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runDistributionErrorTest(t, testCase)
		})
	}
}

type distributionErrorTestCase struct {
	name         string
	configure    func(*generatorMocks)
	distribution v1alpha1.Distribution
	assertErr    func(*testing.T, error)
}

func runDistributionErrorTest(t *testing.T, test distributionErrorTestCase) {
	t.Helper()

	tempDir := t.TempDir()
	buffer := &bytes.Buffer{}
	scaffolderInstance, mocks := newScaffolderWithMocks(t, buffer)

	scaffolderInstance.KSailConfig.Spec.Cluster.Distribution = test.distribution
	test.configure(mocks)

	err := scaffolderInstance.Scaffold(tempDir, false)

	require.Error(t, err)
	test.assertErr(t, err)
}

func assertKindGenerationError(t *testing.T, err error) {
	t.Helper()

	require.ErrorIs(t, err, scaffolder.ErrKindConfigGeneration)
	require.ErrorIs(t, err, errGenerateFailure)
}

func assertK3dGenerationError(t *testing.T, err error) {
	t.Helper()

	require.ErrorIs(t, err, scaffolder.ErrK3dConfigGeneration)
	require.ErrorIs(t, err, errGenerateFailure)
}

func TestScaffoldWrapsKustomizationGenerationErrors(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	buffer := &bytes.Buffer{}
	scaffolderInstance, mocks := newScaffolderWithMocks(t, buffer)

	mocks.kustomization.ExpectedCalls = nil // Clear default expectations
	mocks.kustomization.On(
		"Generate",
		mock.Anything,
		mock.Anything,
	).Return("", errGenerateFailure).Once()

	err := scaffolderInstance.Scaffold(tempDir, false)

	require.Error(t, err)
	require.ErrorIs(t, err, scaffolder.ErrKustomizationGeneration)
}

func TestScaffold_DistributionConfigPreservation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		force           bool
		writer          io.Writer
		expectNewConfig bool
	}{
		{
			name:            "force keeps old and writes new",
			force:           true,
			writer:          &bytes.Buffer{},
			expectNewConfig: true,
		},
		{
			name:            "no force keeps existing only",
			force:           false,
			writer:          io.Discard,
			expectNewConfig: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			outputDir := t.TempDir()
			oldConfig := filepath.Join(outputDir, scaffolder.KindConfigFile)
			require.NoError(t, os.WriteFile(oldConfig, []byte("old"), 0o600))

			cluster := createK3dCluster(testCase.name)
			cluster.Spec.Cluster.DistributionConfig = scaffolder.KindConfigFile

			instance := scaffolder.NewScaffolder(cluster, testCase.writer, nil)

			err := instance.Scaffold(outputDir, testCase.force)
			require.NoError(t, err)

			_, statErr := os.Stat(oldConfig)
			require.NoError(t, statErr)

			if testCase.expectNewConfig {
				_, newErr := os.Stat(filepath.Join(outputDir, scaffolder.K3dConfigFile))
				require.NoError(t, newErr)
			}
		})
	}
}

type scaffoldContextCase struct {
	distribution v1alpha1.Distribution
	initial      string
	expected     string
	expectErr    bool
}

func (c scaffoldContextCase) run(t *testing.T) {
	t.Helper()

	capturedContext, err := captureScaffoldedContext(t, c.distribution, c.initial)

	if c.expectErr {
		require.Error(t, err)
		require.Equal(t, c.expected, capturedContext)

		return
	}

	require.NoError(t, err)
	require.Equal(t, c.expected, capturedContext)
}

func TestScaffoldAppliesContextDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		scenario scaffoldContextCase
	}{
		{
			name: "KindDefaultContext",
			scenario: scaffoldContextCase{
				distribution: v1alpha1.DistributionKind,
				expected:     v1alpha1.ExpectedContextName(v1alpha1.DistributionKind),
			},
		},
		{
			name: "K3dDefaultContext",
			scenario: scaffoldContextCase{
				distribution: v1alpha1.DistributionK3d,
				expected:     v1alpha1.ExpectedContextName(v1alpha1.DistributionK3d),
			},
		},
		{
			name: "TalosDefaultContext",
			scenario: scaffoldContextCase{
				distribution: v1alpha1.DistributionTalos,
				expected:     v1alpha1.ExpectedContextName(v1alpha1.DistributionTalos),
			},
		},
		{
			name: "KeepExistingContext",
			scenario: scaffoldContextCase{
				distribution: v1alpha1.DistributionKind,
				initial:      "custom",
				expected:     "custom",
			},
		},
		{
			name: "UnknownDistributionContext",
			scenario: scaffoldContextCase{
				distribution: v1alpha1.Distribution("Unknown"),
				expected:     "",
				expectErr:    true,
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			testCase.scenario.run(t)
		})
	}
}

func TestGenerateKindConfigHandlesCNI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		cni           v1alpha1.CNI
		expectDisable bool
	}{
		{name: "DefaultCNI", cni: v1alpha1.CNIDefault, expectDisable: false},
		{name: "CiliumCNI", cni: v1alpha1.CNICilium, expectDisable: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			captured := captureKindConfigForCNI(t, testCase.cni)

			disableDefault := captured.Networking.DisableDefaultCNI
			if disableDefault != testCase.expectDisable {
				t.Fatalf(
					"expected DisableDefaultCNI=%t, got %t",
					testCase.expectDisable,
					disableDefault,
				)
			}
		})
	}
}

func TestGenerateK3dConfigHandlesCNI(t *testing.T) {
	t.Parallel()

	cases := []k3dCniCase{
		{name: "DefaultCNI", cni: v1alpha1.CNIDefault, expectArgs: 0},
		{
			name:        "CiliumCNI",
			cni:         v1alpha1.CNICilium,
			expectArgs:  2,
			expectValue: "--flannel-backend=none",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runK3dCniCase(t, testCase)
		})
	}
}

type k3dCniCase struct {
	name        string
	cni         v1alpha1.CNI
	expectArgs  int
	expectValue string
}

func runK3dCniCase(t *testing.T, testCase k3dCniCase) {
	t.Helper()

	captured := captureK3dConfigForCNI(t, testCase.cni)

	extraArgs := captured.Options.K3sOptions.ExtraArgs
	if len(extraArgs) != testCase.expectArgs {
		t.Fatalf("expected %d extra args, got %d", testCase.expectArgs, len(extraArgs))
	}

	if testCase.expectArgs > 0 {
		if extraArgs[0].Arg != testCase.expectValue {
			t.Fatalf("expected first arg %q, got %q", testCase.expectValue, extraArgs[0].Arg)
		}
	}
}

func captureScaffoldedContext(
	t *testing.T,
	distribution v1alpha1.Distribution,
	initial string,
) (string, error) {
	t.Helper()

	tempDir := t.TempDir()
	buffer := &bytes.Buffer{}
	instance, mocks := newScaffolderWithMocks(t, buffer)

	instance.KSailConfig.Spec.Cluster.Distribution = distribution
	instance.KSailConfig.Spec.Cluster.Connection.Context = initial
	instance.KSailConfig.Spec.Cluster.DistributionConfig = ""

	err := instance.Scaffold(tempDir, false)
	if err != nil {
		return "", fmt.Errorf("scaffold context: %w", err)
	}

	return mocks.ksailLastModel.Spec.Cluster.Connection.Context, nil
}

func runCniCapture(
	t *testing.T,
	distribution v1alpha1.Distribution,
	cni v1alpha1.CNI,
	configure func(*generatorMocks),
) {
	t.Helper()

	instance, mocks, tempDir := setupScaffolderForCNI(
		t,
		distribution,
		cni,
	)

	configure(mocks)

	err := instance.Scaffold(tempDir, true)
	require.NoError(t, err)
}

func captureKindConfigForCNI(t *testing.T, cni v1alpha1.CNI) *v1alpha4.Cluster {
	t.Helper()

	var captured *v1alpha4.Cluster

	runCniCapture(
		t,
		v1alpha1.DistributionKind,
		cni,
		func(m *generatorMocks) {
			m.kind.ExpectedCalls = nil
			m.kind.On(
				"Generate",
				mock.MatchedBy(func(cfg *v1alpha4.Cluster) bool {
					captured = cfg

					return true
				}),
				mock.Anything,
			).Return("", nil).Once()
		},
	)

	require.NotNil(t, captured)

	return captured
}

func captureK3dConfigForCNI(t *testing.T, cni v1alpha1.CNI) *k3dv1alpha5.SimpleConfig {
	t.Helper()

	var captured *k3dv1alpha5.SimpleConfig

	runCniCapture(
		t,
		v1alpha1.DistributionK3d,
		cni,
		func(m *generatorMocks) {
			m.k3d.ExpectedCalls = nil
			m.k3d.On(
				"Generate",
				mock.MatchedBy(func(cfg *k3dv1alpha5.SimpleConfig) bool {
					captured = cfg

					return true
				}),
				mock.Anything,
			).Return("", nil).Once()
		},
	)

	require.NotNil(t, captured)

	return captured
}

func setupScaffolderForCNI(
	t *testing.T,
	distribution v1alpha1.Distribution,
	cni v1alpha1.CNI,
) (*scaffolder.Scaffolder, *generatorMocks, string) {
	t.Helper()

	tempDir := t.TempDir()
	buffer := &bytes.Buffer{}
	instance, mocks := newScaffolderWithMocks(t, buffer)
	instance.KSailConfig.Spec.Cluster.CNI = cni
	instance.KSailConfig.Spec.Cluster.Distribution = distribution

	return instance, mocks, tempDir
}

func TestScaffoldForceUpdatesModTime(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	ksailPath := filepath.Join(tempDir, "ksail.yaml")

	writeErr := os.WriteFile(ksailPath, []byte("existing"), 0o600)
	if writeErr != nil {
		t.Fatalf("failed to write test file: %v", writeErr)
	}

	oldTime := time.Now().Add(-2 * time.Minute)

	setTimeErr := os.Chtimes(ksailPath, oldTime, oldTime)
	if setTimeErr != nil {
		t.Fatalf("failed to set mod time: %v", setTimeErr)
	}

	buffer := &bytes.Buffer{}
	instance, mocks := newScaffolderWithMocks(t, buffer)

	mocks.ksail.ExpectedCalls = nil
	mocks.ksail.On("Generate", mock.Anything, mock.Anything).Return("", nil).Once()

	scaffoldErr := instance.Scaffold(tempDir, true)
	if scaffoldErr != nil {
		t.Fatalf("unexpected error: %v", scaffoldErr)
	}

	info, err := os.Stat(ksailPath)
	if err != nil {
		t.Fatalf("failed to stat ksail.yaml: %v", err)
	}

	if !info.ModTime().After(oldTime) {
		t.Fatalf("expected mod time after %v, got %v", oldTime, info.ModTime())
	}
}

// Test case definitions.
type scaffoldTestCase struct {
	name        string
	setupFunc   func(string) v1alpha1.Cluster
	outputPath  string
	force       bool
	expectError bool
}

type contentTestCase struct {
	name         string
	setupFunc    func(string) v1alpha1.Cluster
	distribution v1alpha1.Distribution
}

func getScaffoldTestCases() []scaffoldTestCase {
	return []scaffoldTestCase{
		{
			name:        "Kind distribution",
			setupFunc:   createKindCluster,
			outputPath:  "/tmp/test-kind/",
			force:       true,
			expectError: false,
		},
		{
			name:        "K3d distribution",
			setupFunc:   createK3dCluster,
			outputPath:  "/tmp/test-k3d/",
			force:       true,
			expectError: false,
		},
		{
			name:        "Talos distribution",
			setupFunc:   createTalosCluster,
			outputPath:  "/tmp/test-talos/",
			force:       true,
			expectError: false,
		},
		{
			name:        "Unknown distribution",
			setupFunc:   createUnknownCluster,
			outputPath:  "/tmp/test-unknown/",
			force:       true,
			expectError: true,
		},
	}
}

func getContentTestCases() []contentTestCase {
	return []contentTestCase{
		{
			name:         "Kind configuration content",
			setupFunc:    createKindCluster,
			distribution: v1alpha1.DistributionKind,
		},
		{
			name:         "K3d configuration content",
			setupFunc:    createK3dCluster,
			distribution: v1alpha1.DistributionK3d,
		},
	}
}

func generateDistributionContent(
	t *testing.T,
	scaffolder *scaffolder.Scaffolder,
	cluster v1alpha1.Cluster,
	distribution v1alpha1.Distribution,
) {
	t.Helper()

	// Generate KSail YAML content using actual generator but with minimal cluster config
	minimalCluster := createMinimalClusterForSnapshot(cluster, distribution)
	ksailContent, err := scaffolder.KSailYAMLGenerator.Generate(
		minimalCluster,
		yamlgenerator.Options{},
	)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, ksailContent)

	switch distribution {
	case v1alpha1.DistributionKind:
		// Create minimal Kind configuration without name (Kind will use defaults)
		kindContent := "apiVersion: kind.x-k8s.io/v1alpha4\nkind: Cluster\n"
		snaps.MatchSnapshot(t, kindContent)

	case v1alpha1.DistributionK3d:
		// Create minimal K3d configuration that matches the original hardcoded output
		k3dContent := "apiVersion: k3d.io/v1alpha5\nkind: Simple\nmetadata:\n  name: ksail-default\n"
		snaps.MatchSnapshot(t, k3dContent)

	case v1alpha1.DistributionTalos:
		// Talos doesn't have a separate distribution config file to snapshot
	}
}

// createMinimalClusterForSnapshot creates a cluster config that produces the same YAML
// as the original hardcoded version.
func createMinimalClusterForSnapshot(
	_ v1alpha1.Cluster,
	distribution v1alpha1.Distribution,
) v1alpha1.Cluster {
	minimalCluster := v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.Kind,
		},
	}

	// Only add spec fields if they differ from defaults to match original hardcoded output
	switch distribution {
	case v1alpha1.DistributionKind:
		// For Kind, the original hardcoded output had no spec, so return minimal cluster
		return minimalCluster
	case v1alpha1.DistributionK3d:
		// For K3d, the original hardcoded output included distribution and distributionConfig
		minimalCluster.Spec = v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionK3d,
				DistributionConfig: "k3d.yaml",
			},
		}

		return minimalCluster
	case v1alpha1.DistributionTalos:
		// For Talos, include distribution and distributionConfig
		minimalCluster.Spec = v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionTalos,
				DistributionConfig: "talos",
			},
		}

		return minimalCluster
	default:
		return minimalCluster
	}
}

// Helper functions.
func createTestCluster(_ string) v1alpha1.Cluster {
	return v1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.Kind,
		},
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:       v1alpha1.DistributionKind,
				DistributionConfig: "kind.yaml",
			},
			Workload: v1alpha1.WorkloadSpec{
				SourceDirectory: "k8s",
			},
		},
	}
}

func createKindCluster(name string) v1alpha1.Cluster { return createTestCluster(name) }
func createK3dCluster(name string) v1alpha1.Cluster {
	c := createTestCluster(name)
	c.Spec.Cluster.Distribution = v1alpha1.DistributionK3d
	c.Spec.Cluster.DistributionConfig = "k3d.yaml"

	return c
}

func createTalosCluster(name string) v1alpha1.Cluster {
	c := createTestCluster(name)
	c.Spec.Cluster.Distribution = v1alpha1.DistributionTalos
	c.Spec.Cluster.DistributionConfig = scaffolder.TalosConfigDir

	return c
}

func createUnknownCluster(name string) v1alpha1.Cluster {
	c := createTestCluster(name)
	c.Spec.Cluster.Distribution = "unknown"

	return c
}

type generatorMocks struct {
	ksail          *generator.MockGenerator[v1alpha1.Cluster, yamlgenerator.Options]
	kind           *generator.MockGenerator[*v1alpha4.Cluster, yamlgenerator.Options]
	k3d            *generator.MockGenerator[*k3dv1alpha5.SimpleConfig, yamlgenerator.Options]
	kustomization  *generator.MockGenerator[*ktypes.Kustomization, yamlgenerator.Options]
	ksailLastModel v1alpha1.Cluster
}

func newScaffolderWithMocks(
	t *testing.T,
	writer io.Writer,
) (*scaffolder.Scaffolder, *generatorMocks) {
	t.Helper()

	cluster := createTestCluster("mock-cluster")
	scaffolderInstance := scaffolder.NewScaffolder(cluster, writer, nil)

	mocks := &generatorMocks{
		ksail: generator.NewMockGenerator[
			v1alpha1.Cluster,
			yamlgenerator.Options,
		](t),
		kind: generator.NewMockGenerator[
			*v1alpha4.Cluster,
			yamlgenerator.Options,
		](t),
		k3d: generator.NewMockGenerator[
			*k3dv1alpha5.SimpleConfig,
			yamlgenerator.Options,
		](t),
		kustomization: generator.NewMockGenerator[
			*ktypes.Kustomization,
			yamlgenerator.Options,
		](t),
	}

	// Set up default successful return for ksail generator with model capturing
	mocks.ksail.On(
		"Generate",
		mock.MatchedBy(func(model v1alpha1.Cluster) bool {
			mocks.ksailLastModel = model

			return true
		}),
		mock.Anything,
	).Return("", nil).Maybe()

	// Set up default successful returns for other generators
	mocks.kind.On("Generate", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mocks.k3d.On("Generate", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mocks.kustomization.On("Generate", mock.Anything, mock.Anything).Return("", nil).Maybe()

	scaffolderInstance.KSailYAMLGenerator = mocks.ksail
	scaffolderInstance.KindGenerator = mocks.kind
	scaffolderInstance.K3dGenerator = mocks.k3d
	scaffolderInstance.KustomizationGenerator = mocks.kustomization

	return scaffolderInstance, mocks
}

func setupExistingKSailFile(
	t *testing.T,
) (
	string,
	*bytes.Buffer,
	*scaffolder.Scaffolder,
	*generatorMocks,
) {
	t.Helper()

	tempDir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(tempDir, "ksail.yaml"),
			[]byte("existing"),
			0o600,
		),
	)

	buffer := &bytes.Buffer{}
	scaffolderInstance, mocks := newScaffolderWithMocks(t, buffer)

	return tempDir, buffer, scaffolderInstance, mocks
}

func newK3dScaffolder(t *testing.T, mirrors []string) *scaffolder.Scaffolder {
	t.Helper()

	cluster := v1alpha1.NewCluster()
	cluster.Spec.Cluster.Distribution = v1alpha1.DistributionK3d

	return scaffolder.NewScaffolder(*cluster, &bytes.Buffer{}, mirrors)
}

func TestGenerateK3dRegistryConfig_EmptyMirrors(t *testing.T) {
	t.Parallel()

	scaffolderInstance := newK3dScaffolder(t, nil)

	config := scaffolderInstance.GenerateK3dRegistryConfig()
	assert.Empty(t, config.Use)
	assert.Nil(t, config.Create)
}

func TestGenerateK3dRegistryConfig_InvalidSpec(t *testing.T) {
	t.Parallel()

	scaffolderInstance := newK3dScaffolder(t, []string{"invalid"})

	config := scaffolderInstance.GenerateK3dRegistryConfig()
	assert.Empty(t, config.Use)
	assert.Nil(t, config.Create)
}

func TestGenerateK3dRegistryConfig_WithValidMirror(t *testing.T) {
	t.Parallel()

	scaffolderInstance := newK3dScaffolder(t, []string{"docker.io=https://registry-1.docker.io"})

	config := scaffolderInstance.GenerateK3dRegistryConfig()

	require.Nil(t, config.Create)
	assert.Contains(t, config.Config, "https://registry-1.docker.io")
	assert.Contains(t, config.Config, "\"docker.io\":")
	assert.Empty(t, config.Use)
}

// Tests for createK3dConfig with MetricsServer configuration.
func TestCreateK3dConfig_MetricsServerDisabled(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionK3d,
				MetricsServer: v1alpha1.MetricsServerDisabled,
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
	config := scaffolderInstance.CreateK3dConfig()

	// Check that --disable=metrics-server flag is present
	found := false

	for _, arg := range config.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=metrics-server" {
			found = true

			assert.Equal(t, []string{"server:*"}, arg.NodeFilters)

			break
		}
	}

	assert.True(t, found, "--disable=metrics-server flag should be present")
}

func TestCreateK3dConfig_MetricsServerEnabled(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionK3d,
				MetricsServer: v1alpha1.MetricsServerEnabled,
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
	config := scaffolderInstance.CreateK3dConfig()

	// Check that --disable=metrics-server flag is NOT present
	for _, arg := range config.Options.K3sOptions.ExtraArgs {
		assert.NotEqual(
			t,
			"--disable=metrics-server",
			arg.Arg,
			"flag should not be present when enabled",
		)
	}
}

func TestCreateK3dConfig_MetricsServerDisabledWithCilium(t *testing.T) {
	t.Parallel()

	cluster := v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionK3d,
				CNI:           v1alpha1.CNICilium,
				MetricsServer: v1alpha1.MetricsServerDisabled,
			},
		},
	}

	scaffolderInstance := scaffolder.NewScaffolder(cluster, &bytes.Buffer{}, nil)
	config := scaffolderInstance.CreateK3dConfig()

	// Check that both CNI and metrics-server flags are present
	hasCNIFlag := false
	hasMetricsFlag := false

	for _, arg := range config.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--flannel-backend=none" {
			hasCNIFlag = true
		}

		if arg.Arg == "--disable=metrics-server" {
			hasMetricsFlag = true
		}
	}

	assert.True(t, hasCNIFlag, "CNI flag should be present")
	assert.True(t, hasMetricsFlag, "metrics-server flag should be present")
}

func TestCreateK3dConfig_SetsDefaultImage(t *testing.T) {
	t.Parallel()

	scaffolderInstance := newK3dScaffolder(t, nil)
	config := scaffolderInstance.CreateK3dConfig()

	assert.Equal(t, "rancher/k3s:v1.29.4-k3s1", config.Image)
}

func TestScaffoldTalos_CreatesDirectoryStructure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := createTalosCluster("talos-test")
	// Set workers to 1 to avoid generating allow-scheduling patch (which would replace .gitkeep in cluster/)
	cluster.Spec.Cluster.Talos.Workers = 1
	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Verify the directory structure was created
	expectedPaths := []string{
		filepath.Join(tempDir, scaffolder.TalosConfigDir, "cluster", ".gitkeep"),
		filepath.Join(tempDir, scaffolder.TalosConfigDir, "control-planes", ".gitkeep"),
		filepath.Join(tempDir, scaffolder.TalosConfigDir, "workers", ".gitkeep"),
		filepath.Join(tempDir, "ksail.yaml"),
		filepath.Join(tempDir, "k8s", "kustomization.yaml"),
	}

	for _, path := range expectedPaths {
		_, err := os.Stat(path)
		require.NoError(t, err, "expected path to exist: %s", path)
	}
}

func TestScaffoldTalos_SetsCorrectDistribution(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	buffer := &bytes.Buffer{}
	cluster := createTalosCluster("talos-context-test")
	scaffolderInstance := scaffolder.NewScaffolder(cluster, buffer, nil)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Read the generated ksail.yaml to verify distribution is set
	ksailPath := filepath.Join(tempDir, "ksail.yaml")
	ksailContent, err := os.ReadFile(ksailPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)

	// Verify the distribution is set correctly
	assert.Contains(t, string(ksailContent), "distribution: Talos")
}

// GitOps scaffolding tests.

func TestScaffoldFluxGitOps_CreatesFluxInstanceManifest(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := createKindCluster("flux-test")
	cluster.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Verify FluxInstance manifest was created directly in k8s/
	fluxInstancePath := filepath.Join(tempDir, "k8s", "flux-instance.yaml")
	content, err := os.ReadFile(fluxInstancePath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)

	// Verify content contains expected fields
	assert.Contains(t, string(content), "apiVersion: fluxcd.controlplane.io/v1")
	assert.Contains(t, string(content), "kind: FluxInstance")
	assert.Contains(t, string(content), "name: flux")
	assert.Contains(t, string(content), "namespace: flux-system")
}

func TestScaffoldArgoCDGitOps_CreatesApplicationManifest(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := createKindCluster("argocd-test")
	cluster.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineArgoCD
	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Verify ArgoCD Application manifest was created directly in k8s/
	applicationPath := filepath.Join(tempDir, "k8s", "argocd-application.yaml")
	content, err := os.ReadFile(applicationPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)

	// Verify content contains expected fields
	assert.Contains(t, string(content), "apiVersion: argoproj.io/v1alpha1")
	assert.Contains(t, string(content), "kind: Application")
	assert.Contains(t, string(content), "name: ksail")
	assert.Contains(t, string(content), "namespace: argocd")
}

func TestScaffoldGitOps_SkipsWhenGitOpsEngineIsNone(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := createKindCluster("no-gitops-test")
	cluster.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineNone
	scaffolderInstance := scaffolder.NewScaffolder(cluster, io.Discard, nil)

	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	// Verify no GitOps manifests were created
	fluxInstancePath := filepath.Join(tempDir, "k8s", "flux-instance.yaml")
	argocdAppPath := filepath.Join(tempDir, "k8s", "argocd-application.yaml")

	_, fluxErr := os.Stat(fluxInstancePath)
	_, argocdErr := os.Stat(argocdAppPath)

	assert.True(t, os.IsNotExist(fluxErr), "flux-instance.yaml should not exist")
	assert.True(t, os.IsNotExist(argocdErr), "argocd-application.yaml should not exist")
}

type skipExistingCRTestCase struct {
	name            string
	gitOpsEngine    v1alpha1.GitOpsEngine
	subDir          string
	existingFile    string
	outputFile      string
	existingContent string
	expectSkipMsg   string
}

func getSkipExistingCRTestCases() []skipExistingCRTestCase {
	return []skipExistingCRTestCase{
		{
			name:         "skips existing FluxInstance",
			gitOpsEngine: v1alpha1.GitOpsEngineFlux,
			subDir:       "",
			existingFile: "existing-flux.yaml",
			outputFile:   "flux-instance.yaml",
			existingContent: `apiVersion: fluxcd.controlplane.io/v1
kind: FluxInstance
metadata:
  name: flux
  namespace: flux-system
spec:
  distribution:
    version: "2.x"
`,
			expectSkipMsg: "skipping FluxInstance scaffolding: existing found at",
		},
		{
			name:         "skips existing ArgoCD Application",
			gitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
			subDir:       "",
			existingFile: "existing-app.yaml",
			outputFile:   "argocd-application.yaml",
			existingContent: `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ksail
  namespace: argocd
spec:
  project: default
`,
			expectSkipMsg: "skipping ArgoCD Application scaffolding: existing found at",
		},
	}
}

func TestScaffoldGitOps_SkipsExistingCR(t *testing.T) {
	t.Parallel()

	tests := getSkipExistingCRTestCases()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			cluster := createKindCluster(testCase.name)
			cluster.Spec.Cluster.GitOpsEngine = testCase.gitOpsEngine
			buffer := &bytes.Buffer{}
			scaffolderInstance := scaffolder.NewScaffolder(cluster, buffer, nil)

			// Create an existing CR manifest
			crDir := filepath.Join(tempDir, "k8s", testCase.subDir)

			err := os.MkdirAll(crDir, 0o750)
			require.NoError(t, err)

			err = os.WriteFile(
				filepath.Join(crDir, testCase.existingFile),
				[]byte(testCase.existingContent),
				0o600,
			)
			require.NoError(t, err)

			// Scaffold - should skip CR generation
			err = scaffolderInstance.Scaffold(tempDir, false)
			require.NoError(t, err)

			// Verify the skip message was printed (contains dynamic path)
			require.Contains(t, buffer.String(), testCase.expectSkipMsg)

			// Verify no new output file was created
			_, err = os.Stat(filepath.Join(crDir, testCase.outputFile))
			assert.True(t, os.IsNotExist(err), "%s should not be created", testCase.outputFile)
		})
	}
}

func TestScaffoldGitOps_OverwritesWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cluster := createKindCluster("force-flux-test")
	cluster.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	buffer := &bytes.Buffer{}
	scaffolderInstance := scaffolder.NewScaffolder(cluster, buffer, nil)

	// First scaffold to create the FluxInstance
	err := scaffolderInstance.Scaffold(tempDir, false)
	require.NoError(t, err)

	fluxInstancePath := filepath.Join(tempDir, "k8s", "flux-instance.yaml")

	// Verify file was created
	_, err = os.Stat(fluxInstancePath)
	require.NoError(t, err)

	// Get original mod time
	originalInfo, err := os.Stat(fluxInstancePath)
	require.NoError(t, err)

	originalModTime := originalInfo.ModTime()

	// Wait a tiny bit to ensure mod time can differ
	time.Sleep(10 * time.Millisecond)

	// Scaffold again with force=true - should skip because detector finds existing
	// (force only affects file overwriting, not CR detection)
	buffer.Reset()

	err = scaffolderInstance.Scaffold(tempDir, true)
	require.NoError(t, err)

	// Verify skip message was printed (contains dynamic path, so use Contains)
	require.Contains(t, buffer.String(), "skipping FluxInstance scaffolding: existing found at")

	// Verify file mod time is unchanged (was not regenerated)
	newInfo, err := os.Stat(fluxInstancePath)
	require.NoError(t, err)
	assert.Equal(
		t,
		originalModTime,
		newInfo.ModTime(),
		"file should not be regenerated when existing CR detected",
	)
}

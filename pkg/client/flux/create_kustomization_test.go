package flux_test

import (
	"bytes"
	"maps"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/stretchr/testify/require"
)

const (
	sourceGitRepository = "GitRepository/podinfo"
	pathKustomize       = "./kustomize"
	flagExportKust      = "export"
	appNameKust         = "app"
)

func TestNewCreateKustomizationCmd(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	createCmd := client.CreateCreateCommand("")
	kustomizationCmd := findSubCommand(t, createCmd, "kustomization [name]")

	require.NotNil(t, kustomizationCmd)
	require.Equal(t, "Create or update a Kustomization resource", kustomizationCmd.Short)

	// Verify flags
	sourceKindFlag := kustomizationCmd.Flags().Lookup("source-kind")
	require.NotNil(t, sourceKindFlag)

	sourceFlag := kustomizationCmd.Flags().Lookup("source")
	require.NotNil(t, sourceFlag)

	pathFlag := kustomizationCmd.Flags().Lookup("path")
	require.NotNil(t, pathFlag)

	pruneFlag := kustomizationCmd.Flags().Lookup("prune")
	require.NotNil(t, pruneFlag)

	waitFlag := kustomizationCmd.Flags().Lookup("wait")
	require.NotNil(t, waitFlag)

	targetNamespaceFlag := kustomizationCmd.Flags().Lookup("target-namespace")
	require.NotNil(t, targetNamespaceFlag)

	intervalFlag := kustomizationCmd.Flags().Lookup("interval")
	require.NotNil(t, intervalFlag)

	exportFlag := kustomizationCmd.Flags().Lookup("export")
	require.NotNil(t, exportFlag)

	dependsOnFlag := kustomizationCmd.Flags().Lookup("depends-on")
	require.NotNil(t, dependsOnFlag)

	ignorePathsFlag := kustomizationCmd.Flags().Lookup("ignore-paths")
	require.NotNil(t, ignorePathsFlag)
}

func kustomizationExportTestsBasic() map[string]testCase {
	return map[string]testCase{
		"export basic kustomization": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":       sourceGitRepository,
				"path":         pathKustomize,
				flagExportKust: "true",
			},
		},
		"export with prune": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":       sourceGitRepository,
				"path":         pathKustomize,
				"prune":        "true",
				flagExportKust: "true",
			},
		},
		"export with wait": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":       sourceGitRepository,
				"path":         "./deploy",
				"wait":         "true",
				flagExportKust: "true",
			},
		},
		"export with target namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":           sourceGitRepository,
				"path":             pathKustomize,
				"target-namespace": "production",
				flagExportKust:     "true",
			},
		},
		"export with custom interval": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":       sourceGitRepository,
				"path":         pathKustomize,
				"interval":     "5m",
				flagExportKust: "true",
			},
		},
	}
}

func kustomizationExportTestsAdvanced() map[string]testCase {
	return map[string]testCase{
		"export with namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":       sourceGitRepository,
				"path":         pathKustomize,
				"namespace":    "custom-ns",
				flagExportKust: "true",
			},
		},
		"export with dependencies": {
			args: []string{appNameKust},
			flags: map[string]string{
				"source":       "GitRepository/" + appNameKust,
				"path":         pathKustomize,
				"depends-on":   "infra,database",
				flagExportKust: "true",
			},
		},
		"export with source Kind/name format": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":       sourceGitRepository,
				"path":         "./",
				flagExportKust: "true",
			},
		},
		"export with OCIRepository source": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source-kind":  "OCIRepository",
				"source":       "podinfo",
				"path":         pathKustomize,
				flagExportKust: "true",
			},
		},
		"export with ignore paths": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":       sourceGitRepository,
				"path":         pathKustomize,
				"ignore-paths": "/spec/replicas",
				flagExportKust: "true",
			},
		},
	}
}

func kustomizationExportTests() map[string]testCase {
	tests := make(map[string]testCase)
	maps.Copy(tests, kustomizationExportTestsBasic())
	maps.Copy(tests, kustomizationExportTestsAdvanced())

	return tests
}

func TestCreateKustomization_Export(t *testing.T) {
	t.Parallel()

	for testName, testCase := range kustomizationExportTests() {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			runFluxCommandTest(t, []string{"kustomization"}, testCase)
		})
	}
}

func TestCreateKustomization_ExportIgnorePaths(t *testing.T) {
	t.Parallel()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)
	createCmd.SetArgs([]string{
		"kustomization", "podinfo",
		"--source", sourceGitRepository,
		"--path", pathKustomize,
		"--ignore-paths", "/spec/replicas,/metadata/labels",
		"--" + flagExportKust,
	})

	require.NoError(t, createCmd.Execute())

	output := outBuf.String()
	require.Contains(t, output, "ignore:")
	require.Contains(t, output, "paths:")
	require.Contains(t, output, "- /spec/replicas")
	require.Contains(t, output, "- /metadata/labels")
	// The unscoped rule must not emit a target selector.
	require.NotContains(t, output, "target:")
}

func TestCreateKustomization_InvalidIgnorePath(t *testing.T) {
	t.Parallel()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)
	createCmd.SetArgs([]string{
		"kustomization", "podinfo",
		"--source", sourceGitRepository,
		"--ignore-paths", "spec/replicas",
		"--" + flagExportKust,
	})

	err := createCmd.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, flux.ErrInvalidIgnorePath)
	require.Contains(t, err.Error(), "spec/replicas")
}

func TestCreateKustomization_MissingRequiredSource(t *testing.T) {
	t.Parallel()

	testMissingRequiredFlag(
		t,
		[]string{"kustomization"},
		[]string{"podinfo", "--path", pathKustomize, "--" + flagExportKust},
	)
}

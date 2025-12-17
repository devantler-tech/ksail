package flux_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
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
}

func kustomizationExportTestsBasic() map[string]testCase {
	return map[string]testCase{
		"export basic kustomization": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source": "GitRepository/podinfo",
				"path":   "./kustomize",
				"export": "true",
			},
		},
		"export with prune": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source": "GitRepository/podinfo",
				"path":   "./kustomize",
				"prune":  "true",
				"export": "true",
			},
		},
		"export with wait": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source": "GitRepository/podinfo",
				"path":   "./deploy",
				"wait":   "true",
				"export": "true",
			},
		},
		"export with target namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":           "GitRepository/podinfo",
				"path":             "./kustomize",
				"target-namespace": "production",
				"export":           "true",
			},
		},
		"export with custom interval": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":   "GitRepository/podinfo",
				"path":     "./kustomize",
				"interval": "5m",
				"export":   "true",
			},
		},
	}
}

func kustomizationExportTestsAdvanced() map[string]testCase {
	return map[string]testCase{
		"export with namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source":    "GitRepository/podinfo",
				"path":      "./kustomize",
				"namespace": "custom-ns",
				"export":    "true",
			},
		},
		"export with dependencies": {
			args: []string{"app"},
			flags: map[string]string{
				"source":     "GitRepository/app",
				"path":       "./kustomize",
				"depends-on": "infra,database",
				"export":     "true",
			},
		},
		"export with source Kind/name format": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source": "GitRepository/podinfo",
				"path":   "./",
				"export": "true",
			},
		},
		"export with OCIRepository source": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"source-kind": "OCIRepository",
				"source":      "podinfo",
				"path":        "./kustomize",
				"export":      "true",
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

func TestCreateKustomization_MissingRequiredSource(t *testing.T) {
	t.Parallel()

	testMissingRequiredFlag(
		t,
		[]string{"kustomization"},
		[]string{"podinfo", "--path", "./kustomize", "--export"},
	)
}

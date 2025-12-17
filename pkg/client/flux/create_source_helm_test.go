package flux_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCreateSourceHelmCmd(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	createCmd := client.CreateCreateCommand("")
	sourceCmd := findSourceCommand(t, createCmd)
	helmCmd := findSubCommand(t, sourceCmd, "helm [name]")

	require.NotNil(t, helmCmd)
	require.Equal(t, "Create or update a HelmRepository source", helmCmd.Short)

	// Verify flags
	urlFlag := helmCmd.Flags().Lookup("url")
	require.NotNil(t, urlFlag)

	secretRefFlag := helmCmd.Flags().Lookup("secret-ref")
	require.NotNil(t, secretRefFlag)

	intervalFlag := helmCmd.Flags().Lookup("interval")
	require.NotNil(t, intervalFlag)

	exportFlag := helmCmd.Flags().Lookup("export")
	require.NotNil(t, exportFlag)

	ociProviderFlag := helmCmd.Flags().Lookup("oci-provider")
	require.NotNil(t, ociProviderFlag)

	passCredentialsFlag := helmCmd.Flags().Lookup("pass-credentials")
	require.NotNil(t, passCredentialsFlag)
}

func helmRepositoryExportTestsBasic() map[string]testCase {
	return map[string]testCase{
		"export HTTPS repository": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "https://stefanprodan.github.io/podinfo",
				"export": "true",
			},
		},
		"export OCI repository": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "oci://ghcr.io/stefanprodan/charts",
				"export": "true",
			},
		},
		"export with secret ref": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":        "https://charts.example.com",
				"secret-ref": "helm-credentials",
				"export":     "true",
			},
		},
		"export OCI with provider": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":          "oci://ghcr.io/stefanprodan/charts",
				"oci-provider": "generic",
				"export":       "true",
			},
		},
	}
}

func helmRepositoryExportTestsAdvanced() map[string]testCase {
	return map[string]testCase{
		"export with pass credentials": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":              "https://charts.example.com",
				"secret-ref":       "helm-creds",
				"pass-credentials": "true",
				"export":           "true",
			},
		},
		"export with custom interval": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":      "https://stefanprodan.github.io/podinfo",
				"interval": "10m",
				"export":   "true",
			},
		},
		"export with namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":       "https://stefanprodan.github.io/podinfo",
				"namespace": "custom-ns",
				"export":    "true",
			},
		},
	}
}

func helmRepositoryExportTests() map[string]testCase {
	tests := make(map[string]testCase)
	maps.Copy(tests, helmRepositoryExportTestsBasic())
	maps.Copy(tests, helmRepositoryExportTestsAdvanced())

	return tests
}

func TestCreateHelmRepository_Export(t *testing.T) {
	t.Parallel()

	for testName, testCase := range helmRepositoryExportTests() {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			runFluxCommandTest(t, []string{"source", "helm"}, testCase)
		})
	}
}

func TestCreateHelmRepository_MissingRequiredURL(t *testing.T) {
	t.Parallel()

	testMissingRequiredFlag(
		t,
		[]string{"source", "helm"},
		[]string{"podinfo", "--export"},
	)
}

package flux_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCreateSourceOCICmd(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	createCmd := client.CreateCreateCommand("")
	sourceCmd := findSourceCommand(t, createCmd)
	ociCmd := findSubCommand(t, sourceCmd, "oci [name]")

	require.NotNil(t, ociCmd)
	require.Equal(t, "Create or update an OCIRepository source", ociCmd.Short)

	// Verify flags
	urlFlag := ociCmd.Flags().Lookup("url")
	require.NotNil(t, urlFlag)

	tagFlag := ociCmd.Flags().Lookup("tag")
	require.NotNil(t, tagFlag)

	semverFlag := ociCmd.Flags().Lookup("tag-semver")
	require.NotNil(t, semverFlag)

	digestFlag := ociCmd.Flags().Lookup("digest")
	require.NotNil(t, digestFlag)

	secretRefFlag := ociCmd.Flags().Lookup("secret-ref")
	require.NotNil(t, secretRefFlag)

	providerFlag := ociCmd.Flags().Lookup("provider")
	require.NotNil(t, providerFlag)

	intervalFlag := ociCmd.Flags().Lookup("interval")
	require.NotNil(t, intervalFlag)

	exportFlag := ociCmd.Flags().Lookup("export")
	require.NotNil(t, exportFlag)

	insecureFlag := ociCmd.Flags().Lookup("insecure")
	require.NotNil(t, insecureFlag)
}

func ociRepositoryExportTestsBasic() map[string]testCase {
	return map[string]testCase{
		"export with tag": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "oci://ghcr.io/stefanprodan/manifests/podinfo",
				"tag":    "6.6.2",
				"export": "true",
			},
		},
		"export with semver": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":        "oci://ghcr.io/stefanprodan/manifests/podinfo",
				"tag-semver": ">=6.0.0 <7.0.0",
				"export":     "true",
			},
		},
		"export with digest": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "oci://ghcr.io/stefanprodan/manifests/podinfo",
				"digest": "sha256:abcdef123456",
				"export": "true",
			},
		},
		"export with secret ref": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":        "oci://ghcr.io/stefanprodan/manifests/podinfo",
				"tag":        "6.6.2",
				"secret-ref": "oci-credentials",
				"export":     "true",
			},
		},
		"export with custom provider": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":      "oci://gcr.io/project/manifests",
				"tag":      "v1.0.0",
				"provider": "gcp",
				"export":   "true",
			},
		},
	}
}

func ociRepositoryExportTestsAdvanced() map[string]testCase {
	return map[string]testCase{
		"export with insecure": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":      "oci://localhost:5000/manifests",
				"tag":      "latest",
				"insecure": "true",
				"export":   "true",
			},
		},
		"export with custom interval": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":      "oci://ghcr.io/stefanprodan/manifests/podinfo",
				"tag":      "6.6.2",
				"interval": "15m",
				"export":   "true",
			},
		},
		"export with namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":       "oci://ghcr.io/stefanprodan/manifests/podinfo",
				"tag":       "6.6.2",
				"namespace": "custom-ns",
				"export":    "true",
			},
		},
		"missing reference fails": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "oci://ghcr.io/stefanprodan/manifests/podinfo",
				"export": "true",
			},
			wantErr: true,
			errMsg:  "one of --tag, --tag-semver or --digest is required",
		},
	}
}

func ociRepositoryExportTests() map[string]testCase {
	tests := make(map[string]testCase)
	maps.Copy(tests, ociRepositoryExportTestsBasic())
	maps.Copy(tests, ociRepositoryExportTestsAdvanced())

	return tests
}

func TestCreateOCIRepository_Export(t *testing.T) {
	t.Parallel()

	for testName, testCase := range ociRepositoryExportTests() {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			runFluxCommandTest(t, []string{"source", "oci"}, testCase)
		})
	}
}

func TestCreateOCIRepository_MissingRequiredURL(t *testing.T) {
	t.Parallel()

	testMissingRequiredFlag(
		t,
		[]string{"source", "oci"},
		[]string{"podinfo", "--tag", "v1.0.0", "--export"},
	)
}

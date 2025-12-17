package flux_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCreateSourceGitCmd(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	createCmd := client.CreateCreateCommand("")
	sourceCmd := findSourceCommand(t, createCmd)
	gitCmd := findSubCommand(t, sourceCmd, "git [name]")
	require.Equal(t, "Create or update a GitRepository source", gitCmd.Short)

	// Verify required flags
	urlFlag := gitCmd.Flags().Lookup("url")
	require.NotNil(t, urlFlag)

	branchFlag := gitCmd.Flags().Lookup("branch")
	require.NotNil(t, branchFlag)

	tagFlag := gitCmd.Flags().Lookup("tag")
	require.NotNil(t, tagFlag)

	semverFlag := gitCmd.Flags().Lookup("tag-semver")
	require.NotNil(t, semverFlag)

	commitFlag := gitCmd.Flags().Lookup("commit")
	require.NotNil(t, commitFlag)

	secretRefFlag := gitCmd.Flags().Lookup("secret-ref")
	require.NotNil(t, secretRefFlag)

	intervalFlag := gitCmd.Flags().Lookup("interval")
	require.NotNil(t, intervalFlag)

	exportFlag := gitCmd.Flags().Lookup("export")
	require.NotNil(t, exportFlag)
}

func gitRepositoryExportTestsBasic() map[string]testCase {
	return map[string]testCase{
		"export with branch": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "https://github.com/stefanprodan/podinfo",
				"branch": "master",
				"export": "true",
			},
		},
		"export with tag": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "https://github.com/stefanprodan/podinfo",
				"tag":    "6.6.2",
				"export": "true",
			},
		},
		"export with semver": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":        "https://github.com/stefanprodan/podinfo",
				"tag-semver": ">=6.0.0",
				"export":     "true",
			},
		},
		"export with commit": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":    "https://github.com/stefanprodan/podinfo",
				"commit": "abc123",
				"export": "true",
			},
		},
	}
}

func gitRepositoryExportTestsAdvanced() map[string]testCase {
	return map[string]testCase{
		"export with secret ref": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":        "ssh://git@github.com/stefanprodan/podinfo",
				"branch":     "main",
				"secret-ref": "git-credentials",
				"export":     "true",
			},
		},
		"export with namespace flag": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":       "https://github.com/stefanprodan/podinfo",
				"branch":    "master",
				"namespace": "custom-ns",
				"export":    "true",
			},
		},
		"export with custom interval": {
			args: []string{"podinfo"},
			flags: map[string]string{
				"url":      "https://github.com/stefanprodan/podinfo",
				"branch":   "master",
				"interval": "5m",
				"export":   "true",
			},
		},
	}
}

func gitRepositoryExportTests() map[string]testCase {
	tests := make(map[string]testCase)
	maps.Copy(tests, gitRepositoryExportTestsBasic())
	maps.Copy(tests, gitRepositoryExportTestsAdvanced())

	return tests
}

func TestCreateGitRepository_Export(t *testing.T) {
	t.Parallel()

	for testName, testCase := range gitRepositoryExportTests() {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			runFluxCommandTest(t, []string{"source", "git"}, testCase)
		})
	}
}

func TestCreateGitRepository_MissingRequiredURL(t *testing.T) {
	t.Parallel()

	testMissingRequiredFlag(
		t,
		[]string{"source", "git"},
		[]string{"podinfo", "--branch", "main", "--export"},
	)
}

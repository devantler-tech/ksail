package flux_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	sourceHelmRepository = "HelmRepository/podinfo"
	chartVersionDefault  = "6.6.2"
	intervalDefaultValue = "10m"
	appName              = "app"
	sourceKindGit        = "GitRepository"
	flagChartName        = "chart"
	flagSourceName       = "source"
	flagExportName       = "export"
)

func TestNewCreateHelmReleaseCmd(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	createCmd := client.CreateCreateCommand("")
	helmReleaseCmd := findSubCommand(t, createCmd, "helmrelease [name]")

	require.NotNil(t, helmReleaseCmd)
	require.Equal(t, "Create or update a HelmRelease resource", helmReleaseCmd.Short)
	require.Contains(t, helmReleaseCmd.Aliases, "hr")

	// Verify flags
	sourceKindFlag := helmReleaseCmd.Flags().Lookup("source-kind")
	require.NotNil(t, sourceKindFlag)

	sourceFlag := helmReleaseCmd.Flags().Lookup("source")
	require.NotNil(t, sourceFlag)

	chartFlag := helmReleaseCmd.Flags().Lookup("chart")
	require.NotNil(t, chartFlag)

	chartVersionFlag := helmReleaseCmd.Flags().Lookup("chart-version")
	require.NotNil(t, chartVersionFlag)

	targetNamespaceFlag := helmReleaseCmd.Flags().Lookup("target-namespace")
	require.NotNil(t, targetNamespaceFlag)

	createNamespaceFlag := helmReleaseCmd.Flags().Lookup("create-target-namespace")
	require.NotNil(t, createNamespaceFlag)

	intervalFlag := helmReleaseCmd.Flags().Lookup("interval")
	require.NotNil(t, intervalFlag)

	exportFlag := helmReleaseCmd.Flags().Lookup("export")
	require.NotNil(t, exportFlag)

	dependsOnFlag := helmReleaseCmd.Flags().Lookup("depends-on")
	require.NotNil(t, dependsOnFlag)
}

func helmReleaseExportTestsBasic() map[string]testCase {
	return map[string]testCase{
		"export basic helmrelease": {
			args: []string{"podinfo"},
			flags: map[string]string{
				flagSourceName: sourceHelmRepository,
				flagChartName:  "podinfo",
				flagExportName: "true",
			},
		},
		"export with chart version": {
			args: []string{"podinfo"},
			flags: map[string]string{
				flagSourceName:  sourceHelmRepository,
				flagChartName:   "podinfo",
				"chart-version": chartVersionDefault,
				flagExportName:  "true",
			},
		},
		"export with target namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				flagSourceName:     sourceHelmRepository,
				flagChartName:      "podinfo",
				"target-namespace": "production",
				flagExportName:     "true",
			},
		},
		"export with create namespace": {
			args: []string{"podinfo"},
			flags: map[string]string{
				flagSourceName:            sourceHelmRepository,
				flagChartName:             "podinfo",
				"target-namespace":        "new-ns",
				"create-target-namespace": "true",
				flagExportName:            "true",
			},
		},
		"export with custom interval": {
			args: []string{"podinfo"},
			flags: map[string]string{
				flagSourceName: sourceHelmRepository,
				flagChartName:  "podinfo",
				"interval":     intervalDefaultValue,
				flagExportName: "true",
			},
		},
	}
}

func helmReleaseExportTestsAdvanced() map[string]testCase {
	return map[string]testCase{
		"export with namespace flag": {
			args: []string{"podinfo"},
			flags: map[string]string{
				flagSourceName: sourceHelmRepository,
				flagChartName:  "podinfo",
				"namespace":    "custom-ns",
				flagExportName: "true",
			},
		},
		"export with dependencies": {
			args: []string{appName},
			flags: map[string]string{
				flagSourceName: "HelmRepository/" + appName,
				flagChartName:  appName,
				"depends-on":   "database,cache",
				flagExportName: "true",
			},
		},
		"export with GitRepository source": {
			args: []string{appName},
			flags: map[string]string{
				"source-kind":  sourceKindGit,
				flagSourceName: appName,
				flagChartName:  "./charts/" + appName,
				flagExportName: "true",
			},
		},
		"export with source Kind/name format": {
			args: []string{"podinfo"},
			flags: map[string]string{
				flagSourceName: sourceHelmRepository,
				flagChartName:  "podinfo",
				flagExportName: "true",
			},
		},
		"export with cross-namespace source": {
			args: []string{appName},
			flags: map[string]string{
				flagSourceName: "HelmRepository/charts.flux-system",
				flagChartName:  appName,
				flagExportName: "true",
			},
		},
	}
}

func helmReleaseExportTests() map[string]testCase {
	tests := make(map[string]testCase)
	maps.Copy(tests, helmReleaseExportTestsBasic())
	maps.Copy(tests, helmReleaseExportTestsAdvanced())

	return tests
}

func TestCreateHelmRelease_Export(t *testing.T) {
	t.Parallel()

	for testName, testCase := range helmReleaseExportTests() {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			runFluxCommandTest(t, []string{"helmrelease"}, testCase)
		})
	}
}

func TestCreateHelmRelease_MissingRequiredFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args   []string
		errMsg string
	}{
		"missing source": {
			args:   []string{"podinfo", "--" + flagChartName, "podinfo", "--" + flagExportName},
			errMsg: "required flag(s)",
		},
		"missing chart": {
			args:   []string{"podinfo", "--" + flagSourceName, sourceHelmRepository, "--" + flagExportName},
			errMsg: "required flag(s)",
		},
	}

	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			testCommandError(t, []string{"helmrelease"}, testCase.args, testCase.errMsg)
		})
	}
}

func TestCreateHelmRelease_AliasWorks(t *testing.T) {
	t.Parallel()

	testCommandSuccess(t, []string{
		"hr",
		"podinfo",
		"--" + flagSourceName,
		sourceHelmRepository,
		"--" + flagChartName,
		"podinfo",
		"--" + flagExportName,
	})
}

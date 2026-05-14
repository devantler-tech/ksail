package flux_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	flagChart              = "--chart"
	flagSource             = "--source"
	flagExport             = "--export"
	flagDependsOn          = "--depends-on"
	chartVersion           = "6.6.2"
	intervalDefault        = "10m"
	sourceGitRepoMyRepo    = "GitRepository/my-repo"
	sourceHelmRepoPodinfo  = "HelmRepository/podinfo"
	sourceGitRepoPodinfo   = "GitRepository/podinfo"
	kindHelmRepository     = "HelmRepository"
	kindGitRepository      = "GitRepository"
	chartKeyName           = "chart"
)

// TestCreateSourceCommand_SubCommands verifies that the source command has all
// expected sub-commands: git, helm, and oci.
func TestCreateSourceCommand_SubCommands(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")
	sourceCmd := findSourceCommand(t, cmd)

	subCommands := sourceCmd.Commands()

	expectedUses := map[string]bool{
		"git [name]":  false,
		"helm [name]": false,
		"oci [name]":  false,
	}

	for _, sub := range subCommands {
		if _, ok := expectedUses[sub.Use]; ok {
			expectedUses[sub.Use] = true
		}
	}

	for use, found := range expectedUses {
		assert.True(t, found, "expected sub-command %q in source command", use)
	}
}

// TestNewCreateHelmReleaseCmd_Flags verifies all expected flags are present on
// the helmrelease command.
func TestNewCreateHelmReleaseCmd_Flags(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")

	hrCmd := findSubCommand(t, cmd, "helmrelease [name]")
	require.NotNil(t, hrCmd)

	expectedFlags := []string{
		"source-kind",
		"source",
		"chart",
		"chart-version",
		"target-namespace",
		"create-target-namespace",
		"interval",
		"export",
		"depends-on",
	}

	for _, flagName := range expectedFlags {
		flag := hrCmd.Flags().Lookup(flagName)
		assert.NotNil(t, flag, "expected flag --%s on helmrelease command", flagName)
	}
}

// TestNewCreateHelmReleaseCmd_RequiredFlags verifies that required flags
// cause errors when not provided.
func TestNewCreateHelmReleaseCmd_RequiredFlags(t *testing.T) {
	t.Parallel()

	t.Run("missing source flag", func(t *testing.T) {
		t.Parallel()
		testMissingRequiredFlag(
			t,
			[]string{"helmrelease"},
			[]string{"my-release", flagChart, "my-chart"},
		)
	})

	t.Run("missing chart flag", func(t *testing.T) {
		t.Parallel()
		testMissingRequiredFlag(
			t,
			[]string{"helmrelease"},
			[]string{"my-release", flagSource, sourceHelmRepoPodinfo},
		)
	})
}

// TestNewCreateHelmReleaseCmd_Alias verifies the "hr" alias is configured.
func TestNewCreateHelmReleaseCmd_Alias(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")

	hrCmd := findSubCommand(t, cmd, "helmrelease [name]")
	assert.Contains(t, hrCmd.Aliases, "hr")
}

// TestNewCreateHelmReleaseCmd_Export verifies export mode outputs YAML.
func TestNewCreateHelmReleaseCmd_Export(t *testing.T) {
	t.Parallel()

	testCommandSuccess(t, []string{
		"helmrelease", "podinfo",
		flagSource, sourceHelmRepoPodinfo,
		flagChart, "podinfo",
		flagExport,
	})
}

// TestNewCreateHelmReleaseCmd_ExportWithAllOptions verifies export with
// all optional flags set.
func TestNewCreateHelmReleaseCmd_ExportWithAllOptions(t *testing.T) {
	t.Parallel()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)

	createCmd.SetArgs([]string{
		"helmrelease", "podinfo",
		flagSource, sourceHelmRepoPodinfo,
		flagChart, "podinfo",
		"--chart-version", chartVersion,
		"--target-namespace", "production",
		"--create-target-namespace",
		"--interval", "5m",
		flagDependsOn, "ns/other-release",
		flagExport,
	})

	err := createCmd.Execute()
	require.NoError(t, err)

	output := outBuf.String()
	require.NotEmpty(t, output)
	assert.Contains(t, output, "podinfo")
	assert.Contains(t, output, "spec:")
}

// TestNewCreateKustomizationCmd_Flags verifies all expected flags are present on
// the kustomization command.
func TestNewCreateKustomizationCmd_Flags(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")

	ksCmd := findSubCommand(t, cmd, "kustomization [name]")
	require.NotNil(t, ksCmd)

	expectedFlags := []string{
		"source-kind",
		"source",
		"path",
		"prune",
		"wait",
		"target-namespace",
		"interval",
		"export",
		"depends-on",
	}

	for _, flagName := range expectedFlags {
		flag := ksCmd.Flags().Lookup(flagName)
		assert.NotNil(t, flag, "expected flag --%s on kustomization command", flagName)
	}
}

// TestNewCreateKustomizationCmd_RequiredFlags verifies that missing required
// flags cause errors.
func TestNewCreateKustomizationCmd_RequiredFlags(t *testing.T) {
	t.Parallel()

	testMissingRequiredFlag(
		t,
		[]string{"kustomization"},
		[]string{"my-ks"},
	)
}

// TestNewCreateKustomizationCmd_Export verifies export mode outputs YAML.
func TestNewCreateKustomizationCmd_Export(t *testing.T) {
	t.Parallel()

	testCommandSuccess(t, []string{
		"kustomization", "my-ks",
		flagSource, sourceGitRepoMyRepo,
		flagExport,
	})
}

// TestNewCreateKustomizationCmd_ExportWithAllOptions verifies export with
// all optional flags set.
func TestNewCreateKustomizationCmd_ExportWithAllOptions(t *testing.T) {
	t.Parallel()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)

	createCmd.SetArgs([]string{
		"kustomization", "my-ks",
		flagSource, "OCIRepository/my-oci",
		"--path", "./deploy/production",
		"--prune",
		"--wait",
		"--target-namespace", "prod",
		flagDependsOn, "infra",
		"--interval", intervalDefault,
		flagExport,
	})

	err := createCmd.Execute()
	require.NoError(t, err)

	output := outBuf.String()
	require.NotEmpty(t, output)
	assert.Contains(t, output, "my-ks")
	assert.Contains(t, output, "spec:")
	assert.Contains(t, output, "./deploy/production")
}

// TestCreateCommand_NamespaceFlag verifies the persistent namespace flag
// is present on the root create command.
func TestCreateCommand_NamespaceFlag(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")

	nsFlag := cmd.PersistentFlags().Lookup("namespace")
	require.NotNil(t, nsFlag)
	assert.Equal(t, "flux-system", nsFlag.DefValue)
}

// TestCreateCommand_NamespaceShorthand verifies the -n shorthand works.
func TestCreateCommand_NamespaceShorthand(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")

	nsFlag := cmd.PersistentFlags().ShorthandLookup("n")
	require.NotNil(t, nsFlag)
	assert.Equal(t, "namespace", nsFlag.Name)
}

// TestSourceOCICommand_RequiredFlags verifies the OCI source command's
// required flags.
func TestSourceOCICommand_RequiredFlags(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")
	sourceCmd := findSourceCommand(t, cmd)
	ociCmd := findSubCommand(t, sourceCmd, "oci [name]")

	// Check expected flags
	urlFlag := ociCmd.Flags().Lookup("url")
	require.NotNil(t, urlFlag, "url flag should be present")

	exportFlag := ociCmd.Flags().Lookup("export")
	require.NotNil(t, exportFlag, "export flag should be present")
}

// TestSourceHelmCommand_Flags verifies the Helm source command's flags.
func TestSourceHelmCommand_Flags(t *testing.T) {
	t.Parallel()

	client := setupTestClient()
	cmd := client.CreateCreateCommand("")
	sourceCmd := findSourceCommand(t, cmd)
	helmCmd := findSubCommand(t, sourceCmd, "helm [name]")

	urlFlag := helmCmd.Flags().Lookup("url")
	require.NotNil(t, urlFlag, "url flag should be present")

	exportFlag := helmCmd.Flags().Lookup("export")
	require.NotNil(t, exportFlag, "export flag should be present")
}

// TestHelmReleaseExport_SourceRefParsing verifies source reference parsing
// in export mode (Kind/name.namespace format).
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestHelmReleaseExport_SourceRefParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		wantKind string
	}{
		{
			name:     "Kind/name format",
			source:   sourceHelmRepoPodinfo,
			wantKind: kindHelmRepository,
		},
		{
			name:     "Kind/name.namespace format",
			source:   "GitRepository/flux-system.flux-system",
			wantKind: kindGitRepository,
		},
		{
			name:     "plain name defaults to HelmRepository",
			source:   "podinfo",
			wantKind: kindHelmRepository,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var outBuf bytes.Buffer

			createCmd := setupFluxCommand(&outBuf)

			createCmd.SetArgs([]string{
				"helmrelease", "test-hr",
				flagSource, tt.source,
				flagChart, "test-chart",
				flagExport,
			})

			err := createCmd.Execute()
			require.NoError(t, err)

			output := outBuf.String()
			require.NotEmpty(t, output)
			assert.Contains(t, output, "spec:")
		})
	}
}

// TestKustomizationExport_SourceRefParsing verifies source reference parsing
// in the kustomization command.
//
//nolint:varnamelen // Short names keep this table-driven test readable.
func TestKustomizationExport_SourceRefParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "Kind/name format",
			source: "GitRepository/my-repo",
		},
		{
			name:   "Kind/name.namespace format",
			source: "OCIRepository/my-oci.custom-ns",
		},
		{
			name:   "plain name defaults to GitRepository",
			source: "my-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var outBuf bytes.Buffer

			createCmd := setupFluxCommand(&outBuf)

			createCmd.SetArgs([]string{
				"kustomization", "test-ks",
				"--source", tt.source,
				"--export",
			})

			err := createCmd.Execute()
			require.NoError(t, err)

			output := outBuf.String()
			require.NotEmpty(t, output)
			assert.Contains(t, output, "spec:")
		})
	}
}

// TestKustomizationExport_DependsOn verifies depends-on flag in export mode.
func TestKustomizationExport_DependsOn(t *testing.T) {
	t.Parallel()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)

	createCmd.SetArgs([]string{
		"kustomization", "my-ks",
		"--source", "GitRepository/my-repo",
		"--depends-on", "infra,monitoring",
		"--export",
	})

	err := createCmd.Execute()
	require.NoError(t, err)

	output := outBuf.String()
	require.NotEmpty(t, output)
	assert.Contains(t, output, "dependsOn")
}

// TestHelmReleaseExport_DependsOn verifies depends-on flag in export mode.
func TestHelmReleaseExport_DependsOn(t *testing.T) {
	t.Parallel()

	var outBuf bytes.Buffer

	createCmd := setupFluxCommand(&outBuf)

	createCmd.SetArgs([]string{
		"helmrelease", "my-hr",
		"--source", "HelmRepository/podinfo",
		"--chart", "podinfo",
		"--depends-on", "custom-ns/base-release",
		"--export",
	})

	err := createCmd.Execute()
	require.NoError(t, err)

	output := outBuf.String()
	require.NotEmpty(t, output)
	assert.Contains(t, output, "dependsOn")
}

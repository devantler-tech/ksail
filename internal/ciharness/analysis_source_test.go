package ciharness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/moduleintegrity"
	"github.com/stretchr/testify/require"
)

type analysisSource struct {
	module              string
	version             string
	directory           string
	moduleDirectory     string
	checksum            string
	checkoutChecksum    string
	upstreamAttributes  string
	relocatedVendor     bool
	relocatedDependabot bool
	relocatedGithub     bool
	patchedGoMod        bool
}

// authenticatedAnalysisSources declares exact release bytes and metadata patches.
func authenticatedAnalysisSources() []analysisSource {
	return []analysisSource{
		{
			module:           "github.com/google/cel-go",
			version:          "v0.31.0",
			directory:        "cel-go",
			checksum:         "h1:H0bhpFTqOvmHrBGrWKp7ZlhBm5Hh8PYUEXnwxT1LL7A=",
			checkoutChecksum: "h1:yIEGEbDuDDCD69FZPTdzlltOihVGY66yEyOvyQqnYac=",
			relocatedVendor:  true,
			relocatedGithub:  true,
		},
		{
			module:              "github.com/charmbracelet/glamour",
			version:             "v1.0.0",
			directory:           "glamour",
			checksum:            "h1:AWMLOVFHTsysl4WV8T8QgkQ0s/ZNZo7CiE4WKhk8l08=",
			checkoutChecksum:    "h1:Vot+URz3vKLox+KlbwCU+TEmJOe+7OCP5pslLpEA+p8=",
			upstreamAttributes:  "*.golden linguist-generated=true -text\n*.png filter=lfs diff=lfs merge=lfs -text\n",
			relocatedDependabot: true,
			relocatedGithub:     true,
			patchedGoMod:        true,
		},
		{
			module:           "github.com/anchore/go-macholibre",
			version:          "v0.1.0",
			directory:        "go-macholibre",
			checksum:         "h1:qHbdusBZNcZM/uuKf1Psa9xxAFSoyRTps8GW9gpJgsg=",
			checkoutChecksum: "h1:aDIIEE/XkJa/FhFH/4FrxwxxrpOSxk+NqmRssv68Cdw=",
			relocatedGithub:  true,
			upstreamAttributes: "**/test-fixtures/cache/**/* filter=lfs diff=lfs merge=lfs -text\n" +
				"**/test-fixtures/assets/**/* filter=lfs diff=lfs merge=lfs -text",
		},
		{
			module:           "github.com/kyverno/go-jmespath",
			version:          "v0.4.1-0.20231124160150-95e59c162877",
			directory:        "kyverno-jmespath",
			checksum:         "h1:XOLJNGX/q6MVpI8p8MKvk6jGBMvO4CrdwrizMMSsaRU=",
			checkoutChecksum: "h1:XOLJNGX/q6MVpI8p8MKvk6jGBMvO4CrdwrizMMSsaRU=",
		},
		{
			module:           "github.com/jmespath/go-jmespath",
			version:          "v0.4.1-0.20220621161143-b0104c826a24",
			directory:        "jmespath",
			relocatedGithub:  true,
			checksum:         "h1:liMMTbpW34dhU4az1GN0pTPADwNmvoRSeoZ6PItiqnY=",
			checkoutChecksum: "h1:uVVlH73TQXymO6sZWMeIelXBKTu5AcdUVtE3GyS03+Q=",
		},
		{
			module:           "github.com/charmbracelet/x/ansi",
			version:          "v0.10.2",
			directory:        "ansi",
			moduleDirectory:  "third_party/glamour",
			checksum:         "h1:ith2ArZS0CJG30cIUfID1LXN7ZFXRCww6RUvAPA+Pzw=",
			checkoutChecksum: "h1:ith2ArZS0CJG30cIUfID1LXN7ZFXRCww6RUvAPA+Pzw=",
		},
	}
}

// TestAnalysisDependenciesUseAuthenticatedSource binds selected dependency versions
// to complete published source, independently of mutable upstream fetches.
func TestAnalysisDependenciesUseAuthenticatedSource(t *testing.T) {
	t.Parallel()

	for _, source := range authenticatedAnalysisSources() {
		t.Run(source.module, func(t *testing.T) {
			t.Parallel()
			verifyAnalysisSource(t, source)
		})
	}
}

// TestAnalysisStandaloneReplacementPreservesRootVersion ensures the source
// required by Glamour's standalone analysis cannot downgrade the CLI's graph.
func TestAnalysisStandaloneReplacementPreservesRootVersion(t *testing.T) {
	t.Parallel()

	var selected struct {
		Version string          `json:"Version"` //nolint:tagliatelle // Go command output contract.
		Replace json.RawMessage `json:"Replace"` //nolint:tagliatelle // Go command output contract.
	}
	require.NoError(t, json.Unmarshal(
		authenticatedSourceGoOutput(t, "list", "-m", "-json", "github.com/charmbracelet/x/ansi"),
		&selected,
	))
	require.Equal(t, "v0.11.7", selected.Version)
	require.Empty(t, selected.Replace, "the root graph must retain its selected upstream version")
}

func verifyAnalysisSource(t *testing.T, source analysisSource) {
	t.Helper()

	var selected struct {
		Path    string `json:"Path"`    //nolint:tagliatelle // Go command output contract.
		Version string `json:"Version"` //nolint:tagliatelle // Go command output contract.
		Replace *struct {
			Dir     string `json:"Dir"`     //nolint:tagliatelle // Go command output contract.
			Version string `json:"Version"` //nolint:tagliatelle // Go command output contract.
		} `json:"Replace"` //nolint:tagliatelle // Go command output contract.
	}

	var arguments []string
	if source.moduleDirectory != "" {
		arguments = append(arguments, "-C", source.moduleDirectory)
	}

	arguments = append(arguments, "list", "-m", "-json", source.module)
	require.NoError(t, json.Unmarshal(authenticatedSourceGoOutput(t, arguments...), &selected))
	require.Equal(t, source.module, selected.Path)
	require.Equal(t, source.version, selected.Version)
	require.NotNil(t, selected.Replace, "analysis must resolve the authenticated published source")
	require.Empty(t, selected.Replace.Version)

	expected, err := filepath.Abs(filepath.Join("..", "..", "third_party", source.directory))
	require.NoError(t, err)
	require.Equal(t, expected, selected.Replace.Dir)
	require.NoError(t, moduleintegrity.Verify(
		selected.Replace.Dir, source.module+"@"+source.version, source.checkoutChecksum,
	), "every selected byte must match the authenticated source with its metadata patch")
	verifyPublishedAnalysisSource(t, source, selected.Replace.Dir)
}

// verifyPublishedAnalysisSource restores only declared checkout metadata and
// binds all remaining source bytes to the independently published checksum.
func verifyPublishedAnalysisSource(t *testing.T, source analysisSource, directory string) {
	t.Helper()

	snapshot := filepath.Join(t.TempDir(), "upstream")
	require.NoError(t, os.CopyFS(snapshot, os.DirFS(directory)))

	if source.relocatedDependabot {
		require.NoFileExists(t, filepath.Join(directory, "upstream-github", "dependabot.yml"))
		require.NoError(t, os.Rename(
			filepath.Join(snapshot, "upstream-github", "dependabot.yml.source"),
			filepath.Join(snapshot, "upstream-github", "dependabot.yml"),
		))
	}

	require.NoDirExists(t, filepath.Join(directory, ".github"),
		"archived upstream automation must remain separate from KSail's executable workflows")

	if source.relocatedGithub {
		require.NoError(t, os.Rename(
			filepath.Join(snapshot, "upstream-github"), filepath.Join(snapshot, ".github"),
		))
	}

	if source.patchedGoMod {
		require.NoError(t, os.Remove(filepath.Join(snapshot, "go.mod")))
		require.NoError(t, os.Rename(
			filepath.Join(snapshot, "upstream-go.mod"), filepath.Join(snapshot, "go.mod"),
		))
	}

	if source.upstreamAttributes != "" {
		require.NoError(t, os.WriteFile(
			filepath.Join(snapshot, ".gitattributes"), []byte(source.upstreamAttributes), 0o600,
		))
	}

	if source.relocatedVendor {
		require.NoDirExists(
			t,
			filepath.Join(directory, "vendor"),
			"incomplete vendor mode must remain inactive",
		)
		require.NoError(t, os.Mkdir(filepath.Join(snapshot, "vendor"), 0o700))
		require.NoError(t, os.Rename(
			filepath.Join(
				snapshot,
				"upstream-vendor",
				"modules.txt",
			),
			filepath.Join(snapshot, "vendor", "modules.txt"),
		))
	}

	require.NoError(t, moduleintegrity.Verify(
		snapshot, source.module+"@"+source.version, source.checksum,
	), "restoring declared metadata must reproduce every authenticated upstream byte")
}

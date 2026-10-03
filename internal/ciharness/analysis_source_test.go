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
	patchedGoSum        bool
	sourcePatches       []string
}

// authenticatedAnalysisSources declares exact release bytes and bounded repairs.
func authenticatedAnalysisSources() []analysisSource {
	sources := append(
		authenticatedPrimaryAnalysisSources(),
		authenticatedTransitiveAnalysisSources()...,
	)

	return append(sources, authenticatedRuntimeANSISource(), authenticatedDynamicListenerSource())
}

// authenticatedDynamicListenerSource preserves unavailable dependency test source.
func authenticatedDynamicListenerSource() analysisSource {
	return analysisSource{
		module:           "github.com/rancher/dynamiclistener",
		version:          "v1.27.5",
		directory:        "dynamiclistener",
		checksum:         "h1:FA/s9vbQzGz1Au3BuFvdbBfBBUmHGXGR3xoliwR4qfY=",
		checkoutChecksum: "h1:06Oa4xfsjUJA3Z7x95wr0AsUYafuIY7MuYpR8JJoXqg=",
		relocatedGithub:  true,
	}
}

// authenticatedRuntimeANSISource keeps the shipped parser on its selected version.
func authenticatedRuntimeANSISource() analysisSource {
	return analysisSource{
		module:           "github.com/charmbracelet/x/ansi",
		version:          "v0.11.7",
		directory:        "ansi-runtime",
		checksum:         "h1:kzv1kJvjg2S3r9KHo8hDdHFQLEqn4RBCb39dAYC84jI=",
		checkoutChecksum: "h1:iPNezCZy0sBOmu2IBDjEwzq44xcFUsrqoESVJDQdl9o=",
		sourcePatches:    []string{"util.go", "kitty/options.go"},
	}
}

// authenticatedPrimaryAnalysisSources binds the selected analyzer dependencies.
func authenticatedPrimaryAnalysisSources() []analysisSource {
	return []analysisSource{
		{
			module:           "github.com/google/cel-go",
			version:          "v0.31.0",
			directory:        "cel-go",
			checksum:         "h1:H0bhpFTqOvmHrBGrWKp7ZlhBm5Hh8PYUEXnwxT1LL7A=",
			checkoutChecksum: "h1:+VHNRo4gLsD4ooDPIzatZQ/3272EZ2X5FeSpbnu6NLM=",
			relocatedVendor:  true,
			relocatedGithub:  true,
			sourcePatches:    []string{"common/types/unknown.go"},
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
	}
}

// authenticatedTransitiveAnalysisSources includes standalone dependency graphs.
func authenticatedTransitiveAnalysisSources() []analysisSource {
	return []analysisSource{
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
			checkoutChecksum: "h1:QQEiaO2CuAzsS4wIq7nyf0DvjvwmjWrodGjGtZOC4Fs=",
			sourcePatches:    []string{"util.go", "kitty/options.go"},
		},
		{
			module:           "github.com/redis/go-redis/extra/redisotel/v9",
			version:          "v9.5.3",
			directory:        "redisotel",
			checksum:         "h1:kuvuJL/+MZIEdvtb/kTBRiRgYaOmx1l+lYJyVdrRUOs=",
			checkoutChecksum: "h1:/yV0keEo4cOJoQ8Hk5I2GqFHVYpqxkrLpavcVHxjuNQ=",
			patchedGoMod:     true,
			patchedGoSum:     true,
		},
		{
			module:           "github.com/redis/go-redis/extra/rediscmd/v9",
			version:          "v9.5.3",
			directory:        "rediscmd",
			checksum:         "h1:1/BDligzCa40GTllkDnY3Y5DTHuKCONbB2JcRyIfl20=",
			checkoutChecksum: "h1:10lRBylgtszT0W+LJh04lF13JQafvUx2HoPhoblO/pI=",
			patchedGoMod:     true,
			patchedGoSum:     true,
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
// required by standalone analysis cannot downgrade the CLI's graph.
func TestAnalysisStandaloneReplacementPreservesRootVersion(t *testing.T) {
	t.Parallel()

	for module, version := range map[string]string{
		"github.com/redis/go-redis/v9": "v9.20.1",
	} {
		t.Run(module, func(t *testing.T) {
			t.Parallel()
			verifyUnreplacedRootVersion(t, module, version)
		})
	}

	verifyAnalysisSource(t, authenticatedRuntimeANSISource())
}

// TestAnalysisStandaloneANSIProtocol applies the owned regression suite to
// Glamour's independently selected ANSI version as well as the root parser.
func TestAnalysisStandaloneANSIProtocol(t *testing.T) {
	t.Parallel()

	regressions, err := filepath.Abs("ansi_protocol_test.go")
	require.NoError(t, err)
	authenticatedSourceGoOutput(t,
		"-C", "third_party/ansi", "test", "-count=1", regressions,
	)
}

func verifyUnreplacedRootVersion(t *testing.T, module, version string) {
	t.Helper()

	var selected struct {
		Version string          `json:"Version"` //nolint:tagliatelle // Go command output contract.
		Replace json.RawMessage `json:"Replace"` //nolint:tagliatelle // Go command output contract.
	}
	require.NoError(t, json.Unmarshal(
		authenticatedSourceGoOutput(t, "list", "-m", "-json", module),
		&selected,
	))
	require.Equal(t, version, selected.Version)
	require.Empty(t, selected.Replace, "the root graph must retain its selected upstream version")
}

// TestAnalysisRedisStandaloneUsesAuthenticatedCompanion checks both independent
// modules without replacing the root graph's newer Redis client.
func TestAnalysisRedisStandaloneUsesAuthenticatedCompanion(t *testing.T) {
	t.Parallel()

	for _, source := range authenticatedAnalysisSources() {
		if source.directory == "rediscmd" {
			source.moduleDirectory = "third_party/redisotel"
			verifyAnalysisSource(t, source)
		}
	}

	for _, directory := range []string{"third_party/redisotel", "third_party/rediscmd"} {
		var selected struct {
			Version string          `json:"Version"` //nolint:tagliatelle // Go command output contract.
			Replace json.RawMessage `json:"Replace"` //nolint:tagliatelle // Go command output contract.
		}
		require.NoError(t, json.Unmarshal(authenticatedSourceGoOutput(t,
			"-C", directory, "list", "-m", "-json", "github.com/redis/go-redis/v9",
		), &selected))
		require.Equal(t, "v9.5.3", selected.Version)
		require.Empty(
			t,
			selected.Replace,
			"standalone graphs must select the published Redis client",
		)
	}
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
	), "every selected byte must match the authenticated source with its declared repairs")
	verifyPublishedAnalysisSource(t, source, selected.Replace.Dir)
}

// verifyPublishedAnalysisSource restores only declared metadata and source repairs and
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

	restorePublishedModuleMetadata(t, source, snapshot)
	restorePublishedSourcePatches(t, source, snapshot)

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
	), "restoring declared repairs must reproduce every authenticated upstream byte")
}

func restorePublishedSourcePatches(t *testing.T, source analysisSource, snapshot string) {
	t.Helper()

	for _, relative := range source.sourcePatches {
		require.True(t, filepath.IsLocal(relative))
		require.NoError(t, os.Remove(filepath.Join(snapshot, relative)))
		require.NoError(t, os.Rename(
			filepath.Join(snapshot, "upstream-source", relative+".source"),
			filepath.Join(snapshot, relative),
		))
	}
}

func restorePublishedModuleMetadata(t *testing.T, source analysisSource, snapshot string) {
	t.Helper()

	for filename, patched := range map[string]bool{"go.mod": source.patchedGoMod, "go.sum": source.patchedGoSum} {
		if patched {
			require.NoError(t, os.Remove(filepath.Join(snapshot, filename)))
			require.NoError(t, os.Rename(
				filepath.Join(snapshot, "upstream-"+filename), filepath.Join(snapshot, filename),
			))
		}
	}
}

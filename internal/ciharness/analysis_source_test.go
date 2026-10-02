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
	module             string
	version            string
	directory          string
	checksum           string
	checkoutChecksum   string
	upstreamAttributes string
	relocatedVendor    bool
}

// TestAnalysisDependenciesUseAuthenticatedSource binds selected dependency versions
// to complete published source, independently of mutable upstream fetches.
func TestAnalysisDependenciesUseAuthenticatedSource(t *testing.T) {
	t.Parallel()

	for _, source := range []analysisSource{
		{
			module:           "github.com/google/cel-go",
			version:          "v0.31.0",
			directory:        "cel-go",
			checksum:         "h1:H0bhpFTqOvmHrBGrWKp7ZlhBm5Hh8PYUEXnwxT1LL7A=",
			checkoutChecksum: "h1:Vpn4rhVL3p3ktflMDGRcM/kb2FU2rc0EI4c50HVQwF4=",
			relocatedVendor:  true,
		},
		{
			module:             "github.com/charmbracelet/glamour",
			version:            "v1.0.0",
			directory:          "glamour",
			checksum:           "h1:AWMLOVFHTsysl4WV8T8QgkQ0s/ZNZo7CiE4WKhk8l08=",
			checkoutChecksum:   "h1:X8aVWfbWXAErlE5xaaydb+x6uIy9uI6IBUd7VAL7+S4=",
			upstreamAttributes: "*.golden linguist-generated=true -text\n*.png filter=lfs diff=lfs merge=lfs -text\n",
		},
		{
			module:           "github.com/anchore/go-macholibre",
			version:          "v0.1.0",
			directory:        "go-macholibre",
			checksum:         "h1:qHbdusBZNcZM/uuKf1Psa9xxAFSoyRTps8GW9gpJgsg=",
			checkoutChecksum: "h1:fOuXTj1g04rUXy0kfTWT2SZObNJakIbD6TP41WS31gE=",
			upstreamAttributes: "**/test-fixtures/cache/**/* filter=lfs diff=lfs merge=lfs -text\n" +
				"**/test-fixtures/assets/**/* filter=lfs diff=lfs merge=lfs -text",
		},
	} {
		t.Run(source.module, func(t *testing.T) {
			t.Parallel()
			verifyAnalysisSource(t, source)
		})
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

	require.NoError(t, json.Unmarshal(
		authenticatedSourceGoOutput(t, "list", "-m", "-json", source.module), &selected,
	))
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

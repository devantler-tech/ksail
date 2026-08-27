package hetznertalos_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdaterMovesCoupledDefaultsTogether catches a partial Hetzner catalog
// update: every source field that controls the ISO/Talos compatibility pair
// must move in the same run.
func TestUpdaterMovesCoupledDefaultsTogether(t *testing.T) {
	t.Parallel()

	repoRoot := writeRepositoryFixture(t)
	output, err := runUpdater(t, repoRoot, testFeed)
	require.NoError(t, err, output)

	defaults := readFixtureFile(t, repoRoot, "pkg/apis/cluster/v1alpha1/defaults.go")
	assert.Contains(t, defaults, `DefaultHetznerTalosVersion = "v1.13.2"`)
	assert.Contains(t, defaults, "DefaultTalosISO int64 = 130001")

	options := readFixtureFile(t, repoRoot, "pkg/apis/cluster/v1alpha1/options.go")
	assert.Contains(t, options, `default:"130001" json:"iso,omitzero"`)

	assert.Contains(t, output, "v1.12.4/125127 -> v1.13.2/130001")
}

func TestUpdaterNeverDowngradesTheRepositoryBaseline(t *testing.T) {
	t.Parallel()

	repoRoot := writeRepositoryFixture(t)
	before := snapshotFixture(t, repoRoot)
	output, err := runUpdater(t, repoRoot, oldOnlyFeed)
	require.NoError(t, err, output)

	assert.Equal(t, before, snapshotFixture(t, repoRoot))
	assert.Contains(t, output, "already current at v1.12.4/125127")
}

func TestUpdaterFailsClosedBeforeWritingOnMalformedAnnouncement(t *testing.T) {
	t.Parallel()

	repoRoot := writeRepositoryFixture(t)
	before := snapshotFixture(t, repoRoot)
	output, err := runUpdater(t, repoRoot, malformedFeed)
	require.Error(t, err)

	assert.Equal(t, before, snapshotFixture(t, repoRoot))
	assert.Contains(t, output, "unrecognized ID payload")
}

func TestUpdaterWaitsForCompatibleTalosMachinery(t *testing.T) {
	t.Parallel()

	repoRoot := writeRepositoryFixture(t)
	before := snapshotFixture(t, repoRoot)
	output, err := runUpdater(t, repoRoot, futureFeed)
	require.Error(t, err)

	assert.Equal(t, before, snapshotFixture(t, repoRoot))
	assert.Contains(t, output, "update the dependency first")
}

func writeRepositoryFixture(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	writeFixtureFile(t, repoRoot, "go.mod", `module example.com/fixture

go 1.26.0

require github.com/siderolabs/talos/pkg/machinery v1.14.0-alpha.2
`)
	writeFixtureFile(t, repoRoot, "pkg/apis/cluster/v1alpha1/defaults.go", `package v1alpha1

const (
	DefaultHetznerTalosVersion = "v1.12.4"
	DefaultTalosISO int64 = 125127
)
`)
	writeFixtureFile(t, repoRoot, "pkg/apis/cluster/v1alpha1/options.go", `package v1alpha1

type OptionsTalos struct {
	ISO int64 `+"`"+`default:"125127" json:"iso,omitzero"`+"`"+`
}
`)

	return repoRoot
}

func runUpdater(t *testing.T, repoRoot, feed string) (string, error) {
	t.Helper()

	feedPath := filepath.Join(t.TempDir(), "feed.rss")
	require.NoError(t, os.WriteFile(feedPath, []byte(feed), 0o600))

	// The test controls every argument passed to the subprocess.
	cmd := exec.CommandContext( //nolint:gosec
		t.Context(),
		"go", "run", "./cmd", "--feed-file", feedPath, "--root", repoRoot,
	)
	output, err := cmd.CombinedOutput()

	return string(output), err
}

func snapshotFixture(t *testing.T, repoRoot string) map[string]string {
	t.Helper()

	paths := []string{
		"pkg/apis/cluster/v1alpha1/defaults.go",
		"pkg/apis/cluster/v1alpha1/options.go",
	}
	snapshot := make(map[string]string, len(paths))

	for _, path := range paths {
		snapshot[path] = readFixtureFile(t, repoRoot, path)
	}

	return snapshot
}

func writeFixtureFile(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func readFixtureFile(t *testing.T, root, name string) string {
	t.Helper()

	content, err := os.ReadFile( //nolint:gosec // Test fixtures live beneath t.TempDir.
		filepath.Join(root, filepath.FromSlash(name)),
	)
	require.NoError(t, err)

	return string(content)
}

const testFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <item>
      <title>Talos Linux v1.13.2 ISO now available</title>
      <link>https://docs.hetzner.cloud/changelog#2026-09-02-talos-linux-v1132</link>
      <content:encoded><![CDATA[
        <p>The ISO <code>Talos Linux 1.13.2</code>
        (IDs <code>130001</code> (x86) &#x26; <code>130002</code> (arm))
        is now available as ISO for all Cloud Servers.</p>
      ]]></content:encoded>
    </item>
    <item>
      <title>Talos Linux v1.11.2 ISO now available</title>
      <link>https://docs.hetzner.cloud/changelog#2025-10-09-talos-linux-v1112</link>
      <content:encoded><![CDATA[
        <p>The ISO <code>Talos Linux 1.11.2</code>
        (IDs <code>122630</code> (x86) &#x26; <code>122629</code> (arm))
        is now available as ISO for all Cloud Servers.</p>
      ]]></content:encoded>
    </item>
  </channel>
</rss>
`

const oldOnlyFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <item>
      <title>Talos Linux v1.11.2 ISO now available</title>
      <link>https://docs.hetzner.cloud/changelog#2025-10-09-talos-linux-v1112</link>
      <content:encoded><![CDATA[
        <p>The ISO <code>Talos Linux 1.11.2</code>
        (IDs <code>122630</code> (x86) &#x26; <code>122629</code> (arm))
        is now available as ISO for all Cloud Servers.</p>
      ]]></content:encoded>
    </item>
  </channel>
</rss>
`

const malformedFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <item>
      <title>Talos Linux v1.13.2 ISO now available</title>
      <link>https://docs.hetzner.cloud/changelog#2026-09-02-talos-linux-v1132</link>
      <content:encoded><![CDATA[<p>The announcement format changed.</p>]]></content:encoded>
    </item>
  </channel>
</rss>
`

const futureFeed = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <item>
      <title>Talos Linux v9.0.0 ISO now available</title>
      <link>https://docs.hetzner.cloud/changelog#future-talos-linux-v900</link>
      <content:encoded><![CDATA[
        <p>The ISO <code>Talos Linux 9.0.0</code>
        (IDs <code>900001</code> (x86) &#x26; <code>900002</code> (arm))
        is now available as ISO for all Cloud Servers.</p>
      ]]></content:encoded>
    </item>
  </channel>
</rss>
`

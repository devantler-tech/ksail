// Package snapshottest centralizes CI-safe snapshot suite execution helpers.
package snapshottest

import (
	"os"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

// Run executes the test suite and only cleans snapshots after successful runs
// when snapshot cleanup is explicitly enabled outside CI.
func Run(m *testing.M, opts snaps.CleanOpts) int {
	exitCode := m.Run()

	if exitCode != 0 ||
		!snapshotCleanupEnabled() ||
		os.Getenv("CI") != "" ||
		os.Getenv("GITHUB_ACTIONS") != "" {
		return exitCode
	}

	_, err := snaps.Clean(m, opts)
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		return 1
	}

	return exitCode
}

func snapshotCleanupEnabled() bool {
	return os.Getenv("SNAPSHOT_CLEAN") == "1"
}

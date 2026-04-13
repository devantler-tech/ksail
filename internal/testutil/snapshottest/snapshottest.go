// Package snapshottest centralizes CI-safe snapshot suite execution helpers.
package snapshottest

import (
	"os"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

// Run executes the test suite and only cleans snapshots outside CI, where
// rewrite-in-place cleanup would otherwise trip source-overwrite protections.
func Run(m *testing.M, opts snaps.CleanOpts) int {
	exitCode := m.Run()

	if exitCode != 0 || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return exitCode
	}

	_, err := snaps.Clean(m, opts)
	if err != nil {
		_, _ = os.Stderr.WriteString("failed to clean snapshots: " + err.Error() + "\n")

		return 1
	}

	return exitCode
}

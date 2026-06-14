package talosprovisioner_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
)

// TestMain redirects $HOME to a throwaway directory so tests in this package
// never read from or write to the developer's real ~/.kube/config or ~/.ksail/.
func TestMain(m *testing.M) {
	os.Exit(homeenv.Run(m))
}

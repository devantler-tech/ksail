package clusterapi_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
)

// TestMain redirects $HOME to a throwaway directory so tests in this package
// never read from or write to the developer's real ~/.kube/config or ~/.ksail/.
// Lifecycle calls such as Delete finish in a background goroutine that can
// outlive a test's own HOME override, so the isolation has to cover the suite.
func TestMain(m *testing.M) {
	os.Exit(homeenv.Run(m))
}

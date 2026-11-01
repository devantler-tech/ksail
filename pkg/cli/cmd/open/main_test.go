package open_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
	"github.com/zalando/go-keyring"
)

// Web commands construct the real UI server. Isolate its credential backend
// before any test runs, alongside the existing home-directory test boundary.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(homeenv.Run(m))
}

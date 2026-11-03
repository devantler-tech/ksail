package uiserver_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
	"github.com/zalando/go-keyring"
)

// Constructor tests exercise real settings wiring with a process-local store,
// never the developer's native credential backend or home-directory state.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(homeenv.Run(m))
}

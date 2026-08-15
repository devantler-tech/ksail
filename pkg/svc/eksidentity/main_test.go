package eksidentity_test

import (
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/homeenv"
)

func TestMain(m *testing.M) {
	os.Exit(homeenv.Run(m))
}

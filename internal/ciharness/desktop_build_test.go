package ciharness_test

import (
	"go/build"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The native entry point must require an explicit opt-in on every supported OS.
// Otherwise a root go test ./... or static CLI build can acquire CGO/webview needs.
func TestDesktopRequiresBuildTag(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"darwin", "linux", "windows"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			context := build.Default
			context.GOOS = target
			context.BuildTags = nil
			context.CgoEnabled = true
			path := filepath.Join("..", "..", "desktop")

			_, err := context.ImportDir(path, 0)

			var noGo *build.NoGoError
			require.ErrorAs(t, err, &noGo, "default builds must exclude desktop source AND tests")

			context.BuildTags = []string{"desktop"}
			pkg, err := context.ImportDir(path, 0)
			require.NoError(t, err)
			assert.Equal(t, "main", pkg.Name)
			assert.Contains(t, pkg.GoFiles, "main.go")
			assert.Contains(t, pkg.TestGoFiles, "window_state_test.go")

			if target == "darwin" {
				assert.Contains(t, pkg.GoFiles, "env_darwin.go")
				assert.Contains(t, pkg.TestGoFiles, "env_darwin_test.go")
				assert.NotContains(t, pkg.GoFiles, "env_other.go")
			} else {
				assert.Contains(t, pkg.GoFiles, "env_other.go")
				assert.NotContains(t, pkg.GoFiles, "env_darwin.go")
				assert.NotContains(t, pkg.TestGoFiles, "env_darwin_test.go")
			}
		})
	}
}

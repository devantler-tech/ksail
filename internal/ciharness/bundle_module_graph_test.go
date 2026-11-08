package ciharness_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Generated bundle imports must not change the module graph just before release
// compilation. Exercise Go's real tidy with the tracked dependency declaration
// and a minimal local codec module, independent of network and module caches.
// This fixture checks import classification; actual SDK generation is separate
// integration evidence rather than a claim made by the fixture package.
func TestGeneratedBundleKeepsModuleGraphStable(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	fixture := t.TempDir()
	fixtureFiles, err := os.OpenRoot(fixture)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fixtureFiles.Close()) })

	sourceFiles, err := os.OpenRoot(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sourceFiles.Close()) })

	const permissions = 0o600

	const module = `module example.test/bundlegraph

go 1.27.0

require github.com/klauspost/compress v0.0.0

replace github.com/klauspost/compress => ./codec
`
	require.NoError(t, fixtureFiles.WriteFile("go.mod", []byte(module), permissions))
	require.NoError(t, fixtureFiles.WriteFile("main.go",
		[]byte("package main\nfunc main() {}\n"), permissions))
	require.NoError(t, fixtureFiles.MkdirAll("codec/zstd", 0o700))
	require.NoError(t, fixtureFiles.WriteFile("codec/go.mod",
		[]byte("module github.com/klauspost/compress\n\ngo 1.27.0\n"), permissions))
	require.NoError(t, fixtureFiles.WriteFile("codec/zstd/codec.go",
		[]byte("package zstd\n"), permissions))

	declaration, err := sourceFiles.ReadFile("internal/copilotbundledeps/deps.go")
	if !os.IsNotExist(err) {
		require.NoError(t, err)

		require.NoError(t, fixtureFiles.MkdirAll("internal/copilotbundledeps", 0o700))
		require.NoError(
			t,
			fixtureFiles.WriteFile("internal/copilotbundledeps/deps.go", declaration, permissions),
		)
	}

	tidyBundleFixture(t, fixture)

	before, err := fixtureFiles.ReadFile("go.mod")
	require.NoError(t, err)

	// This is the direct dependency introduced by the pinned SDK's generated
	// bundle. The real producer is also exercised before publishing the fix.
	require.NoError(t, fixtureFiles.WriteFile("zcopilot_probe.go",
		[]byte("package main\nimport _ \"github.com/klauspost/compress/zstd\"\n"), permissions))
	tidyBundleFixture(t, fixture)

	after, err := fixtureFiles.ReadFile("go.mod")
	require.NoError(t, err)
	require.Equal(t, string(before), string(after),
		"adding the generated bundle changed go.mod before release compilation")
}

func tidyBundleFixture(t *testing.T, directory string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	command := exec.CommandContext(ctx, "go", "mod", "tidy")
	command.Dir = directory

	command.Env = append(
		os.Environ(),
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOFLAGS=",
		"GOENV=off",
		"GOMODCACHE="+filepath.Join(directory, "module-cache"),
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

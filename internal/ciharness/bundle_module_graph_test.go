package ciharness_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Generated bundle imports must not change the module graph just before release
// compilation. Exercise Go's real tidy with the tracked dependency declaration
// and locally resolved libraries; no network or test doubles are used.
func TestGeneratedBundleKeepsModuleGraphStable(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	modules := resolvedBundleModules(t, root)
	fixture := t.TempDir()
	fixtureFiles, err := os.OpenRoot(fixture)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fixtureFiles.Close()) })

	sourceFiles, err := os.OpenRoot(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sourceFiles.Close()) })

	var module strings.Builder
	module.WriteString("module example.test/bundlegraph\n\ngo 1.27.0\n")

	for _, dependency := range modules {
		_, err := fmt.Fprintf(&module, "require %s %s\nreplace %s => %q\n",
			dependency.Path, dependency.Version, dependency.Path, dependency.Dir)
		require.NoError(t, err)
	}

	const permissions = 0o600

	require.NoError(t, fixtureFiles.WriteFile("go.mod", []byte(module.String()), permissions))
	require.NoError(t, fixtureFiles.WriteFile("main.go",
		[]byte("package main\nfunc main() {}\n"), permissions))

	declaration, err := sourceFiles.ReadFile("internal/copilotbundledeps/deps.go")
	if !os.IsNotExist(err) {
		require.NoError(t, err)

		require.NoError(t, fixtureFiles.MkdirAll("internal/copilotbundledeps", 0o700))
		require.NoError(
			t,
			fixtureFiles.WriteFile("internal/copilotbundledeps/deps.go", declaration, permissions),
		)
	}

	runBundleGo(t, fixture, "tidy")

	before, err := fixtureFiles.ReadFile("go.mod")
	require.NoError(t, err)

	// This is the direct dependency introduced by the pinned SDK's generated
	// bundle. The real producer is also exercised before publishing the fix.
	require.NoError(t, fixtureFiles.WriteFile("zcopilot_probe.go",
		[]byte("package main\nimport _ \"github.com/klauspost/compress/zstd\"\n"), permissions))
	runBundleGo(t, fixture, "tidy")

	after, err := fixtureFiles.ReadFile("go.mod")
	require.NoError(t, err)
	require.Equal(t, string(before), string(after),
		"adding the generated bundle changed go.mod before release compilation")
}

type bundleModule struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Dir     string `json:"dir"`
}

func resolvedBundleModules(t *testing.T, root string) []bundleModule {
	t.Helper()
	output := runBundleGo(t, root, "list-modules")
	decoder := json.NewDecoder(bytes.NewReader(output))

	modules := make([]bundleModule, 2)
	for index := range modules {
		require.NoError(t, decoder.Decode(&modules[index]))
		require.NotEmpty(t, modules[index].Dir)
		require.NotEmpty(t, modules[index].Version)
	}

	return modules
}

func runBundleGo(t *testing.T, directory, operation string) []byte {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	var command *exec.Cmd

	switch operation {
	case "tidy":
		command = exec.CommandContext(ctx, "go", "mod", "tidy")
	case "list-modules":
		command = exec.CommandContext(ctx, "go", "list", "-m", "-json",
			"github.com/klauspost/compress", "github.com/klauspost/cpuid/v2")
	default:
		t.Fatalf("unsupported Go operation %q", operation)
	}

	command.Dir = directory

	command.Env = append(
		os.Environ(),
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOFLAGS=",
		"GOENV=off",
	)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)

	return output
}

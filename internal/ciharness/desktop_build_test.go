package ciharness_test

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// Run the composite's actual stage command with a downloaded action tree
// distinct from the checkout that will compile the embedded UI.
func TestDesktopStageWebUI(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	setup := readCompositeAction(t, ".github/actions/setup-desktop-build/action.yml")
	stage := findHarnessStep(t, setup.Runs.Steps, "🏗️ Build and stage web UI")

	const previousIndex = "previous staged UI"

	for _, scenario := range []struct {
		name      string
		composite bool
		index     []byte
		wantError bool
	}{
		{name: "release checkout", composite: true, index: []byte("release UI")},
		{name: "script root by default", index: []byte("local UI")},
		{name: "empty index", composite: true, index: []byte{}, wantError: true},
		{name: "missing index", composite: true, wantError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			actionRoot := filepath.Join(directory, "downloaded action")
			workspace := filepath.Join(directory, "release checkout")
			command := `bash "$GITHUB_ACTION_PATH/../../../scripts/stage-webui.sh"`

			target, untouched := actionRoot, workspace
			if scenario.composite {
				target, untouched = workspace, actionRoot
				command = stage.Run
			}

			stageWebUIFixture(t, target, previousIndex, scenario.index)
			stageWebUIFixture(t, untouched, previousIndex, []byte("wrong checkout UI"))

			output, err := runDesktopWebUIStage(t, command, directory, actionRoot, workspace)

			want := string(scenario.index)
			if scenario.wantError {
				require.Error(t, err, "a build without a usable index must fail: %s", output)
				assert.Contains(t, string(output), "did not produce a nonempty")

				want = previousIndex
			} else {
				require.NoError(t, err, "%s", output)
			}

			assert.Equal(t, want, stagedDesktopIndex(t, target), "target checkout index")
			assert.Equal(t, previousIndex, stagedDesktopIndex(t, untouched), "other checkout index")
		})
	}
}

func runDesktopWebUIStage(
	t *testing.T,
	command, directory, actionRoot, workspace string,
) ([]byte, error) {
	t.Helper()

	actionPath := filepath.Join(actionRoot, ".github", "actions", "setup-desktop-build")
	require.NoError(t, os.MkdirAll(actionPath, 0o700))

	script := filepath.Join(actionRoot, "scripts", "stage-webui.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(script), 0o700))
	require.NoError(t, os.WriteFile(script, readRepoFile(t, "scripts/stage-webui.sh"), 0o600))

	// Substitute only the network-dependent build. Staging uses the real script and filesystem.
	writeExecutableStub(t, filepath.Join(directory, "npm"), `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
"--prefix web/ui ci") ;;
"--prefix web/ui run build")
	mkdir -p web/ui/dist
	if [[ -f web/ui/fixture-index.html ]]; then cp web/ui/fixture-index.html web/ui/dist/index.html; fi
	;;
*) exit 64 ;;
esac
`)

	cmd := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	cmd.Stdin = strings.NewReader(command)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(),
		"PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_ACTION_PATH="+actionPath,
		"GITHUB_WORKSPACE="+workspace,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("stage the desktop web UI: %w", err)
	}

	return output, nil
}

func stageWebUIFixture(t *testing.T, root, previousIndex string, index []byte) {
	t.Helper()

	uiSource := filepath.Join(root, "web", "ui")
	require.NoError(t, os.MkdirAll(uiSource, 0o700))

	stage := filepath.Join(root, "pkg", "webui", "dist")
	require.NoError(t, os.MkdirAll(stage, 0o700))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(stage, "index.html"), []byte(previousIndex), 0o600),
	)

	if index != nil {
		require.NoError(
			t,
			os.WriteFile(filepath.Join(uiSource, "fixture-index.html"), index, 0o600),
		)
	}
}

func stagedDesktopIndex(t *testing.T, directory string) string {
	t.Helper()

	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	index, err := root.ReadFile(filepath.Join("pkg", "webui", "dist", "index.html"))
	require.NoError(t, err)

	return string(index)
}

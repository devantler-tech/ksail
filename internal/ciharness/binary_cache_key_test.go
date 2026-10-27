package ciharness_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Execute the actual action's key step against a small, dependency-free module.
// The legacy caller hashes only Go/module files, so an embedded Dockerfile
// mutation must reproduce unchanged-key reuse before this action is repaired.
func TestBinaryCacheKeyIncludesEmbeddedDockerfile(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)
	before := binaryKeyFixture(t, directory)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "assets", "Dockerfile"),
		[]byte("FROM example.invalid/fixture:v2\n"), 0o600))
	after := binaryKeyFixture(t, directory)
	require.NotEqual(
		t,
		before,
		after,
		"changing a compiled embedded asset must reject the old binary cache",
	)
}

func TestBinaryCacheKeyTracksEffectiveBuildInputs(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]struct {
		path, content string
		changes       bool
	}{
		"chart":            {"assets/Chart.yaml", "version: 2.0.0\n", true},
		"web UI":           {"assets/app.js", "console.log('new interface');\n", true},
		"provisioning":     {"assets/manifest.yaml", "kind: ConfigMap\n", true},
		"compiled source":  {"version.go", "package main\nconst version = 2\n", true},
		"generated source": {"docs_generated.go", "package main\nconst docs = `updated`\n", true},
		"ordinary test":    {"main_test.go", "package main\n// Test-only edit.\n", false},
		"embedded test":    {"assets/example_test.go", "embedded test text\n", true},
		"ignored source":   {"ignored.go", "//go:build ignore\n\npackage main\nconst ignored = 1\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			directory := newBinaryKeyFixture(t)
			before := binaryKeyFixture(t, directory)
			require.NoError(
				t,
				os.WriteFile(filepath.Join(directory, change.path), []byte(change.content), 0o600),
			)

			after := binaryKeyFixture(t, directory)
			if change.changes {
				require.NotEqual(t, before, after)
			} else {
				require.Equal(t, before, after)
			}
		})
	}
}

func TestBinaryCacheKeyIsIndependentOfCheckoutPath(t *testing.T) {
	t.Parallel()
	first := binaryKeyFixture(t, newBinaryKeyFixture(t))
	second := binaryKeyFixture(t, newBinaryKeyFixture(t))
	require.Equal(t, first, second)
}

func TestBinaryCacheKeyTracksTargetAndBundledCLI(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)

	before := binaryKeyFixture(t, directory)
	for _, change := range []string{
		"RUNNER_ARCH=other", "COPILOT_CLI_VERSION=1.0.88", "GOOS=darwin",
		"GOARCH=arm64", "GOFLAGS=-tags=fixture", "ImageVersion=changed",
	} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			require.NotEqual(t, before, binaryKeyFixture(t, directory, change))
		})
	}
}

func newBinaryKeyFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(directory, "assets"), 0o700))

	files := map[string]string{
		"go.mod":            "module example.invalid/binary-key\n\ngo 1.23.0\n",
		"go.sum":            "",
		"main.go":           "package main\nimport \"embed\"\n//go:embed assets/*\nvar assets embed.FS\nfunc main() {}\n",
		"assets/Dockerfile": "FROM example.invalid/fixture:v1\n",
	}
	for name, contents := range files {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600))
	}

	return directory
}

func binaryKeyFixture(t *testing.T, directory string, overrides ...string) string {
	t.Helper()
	key, err := binaryKeyFixtureResult(t, directory, overrides...)
	require.NoError(t, err)

	return key
}

func binaryKeyFixtureResult(t *testing.T, directory string, overrides ...string) (string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/cache-ksail-binary/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🔑 Compute ksail cache key")
	outputDirectory := t.TempDir()
	output := filepath.Join(outputDirectory, "output")
	outputRoot, err := os.OpenRoot(outputDirectory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, outputRoot.Close()) })

	actionPath, err := filepath.Abs(
		filepath.Join("..", "..", ".github", "actions", "cache-ksail-binary"),
	)
	require.NoError(t, err)
	versionCommand := exec.CommandContext(t.Context(), "go", "env", "GOVERSION")
	version, err := versionCommand.Output()
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Dir = directory
	command.Stdin = strings.NewReader(step.Run)

	command.Env = append(
		os.Environ(),
		"GOWORK=off",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
		"RUNNER_OS=Linux",
		"RUNNER_ARCH=X64",
		"GOOS=linux",
		"GOARCH=amd64",
		"GO_VERSION="+strings.TrimPrefix(strings.TrimSpace(string(version)), "go"),
		"SOURCE_HASH="+legacyBinarySourceHash(t, directory),
		"COPILOT_CLI_VERSION=1.0.87",
		"GITHUB_ACTION_PATH="+actionPath,
		"GITHUB_OUTPUT="+output,
	)
	command.Env = append(command.Env, overrides...)

	diagnostic, runErr := command.CombinedOutput()
	if runErr != nil {
		return string(diagnostic), fmt.Errorf("cache-key step: %w: %s", runErr, diagnostic)
	}

	contents, err := outputRoot.ReadFile("output")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(contents), "value="))

	return strings.TrimSpace(strings.TrimPrefix(string(contents), "value=")), nil
}

func TestBinaryCacheKeyRefusesUnhashedOverlay(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)
	alternate := filepath.Join(t.TempDir(), "alternate.go")
	require.NoError(t, os.WriteFile(alternate, []byte("package main\nfunc main() {}\n"), 0o600))
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	contents := fmt.Sprintf(`{"Replace":{%q:%q}}`, filepath.Join(directory, "main.go"), alternate)
	require.NoError(t, os.WriteFile(overlay, []byte(contents), 0o600))
	output, err := binaryKeyFixtureResult(t, directory, "GOFLAGS=-overlay="+overlay)
	require.Error(
		t,
		err,
		"file-based compiler overrides must not select a key that omits their contents",
	)
	require.NotContains(t, output, "ksail-bin-v2-")
}

func TestBinaryCacheKeyRefusesIncompleteInputs(t *testing.T) {
	t.Parallel()

	for _, fault := range []string{
		"missing-embed", "missing-module", "unresolved-import", "wrong-toolchain", "escaping-source",
	} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			directory := newBinaryKeyFixture(t)

			var overrides []string

			switch fault {
			case "missing-embed":
				require.NoError(t, os.Remove(filepath.Join(directory, "assets", "Dockerfile")))
			case "missing-module":
				require.NoError(t, os.Remove(filepath.Join(directory, "go.mod")))
			case "unresolved-import":
				require.NoError(t, os.WriteFile(
					filepath.Join(directory, "main.go"),
					[]byte(
						"package main\nimport _ \"example.invalid/unavailable\"\nfunc main() {}\n",
					),
					0o600,
				))
			case "wrong-toolchain":
				overrides = []string{"GO_VERSION=1.0.0"}
			case "escaping-source":
				target := filepath.Join(t.TempDir(), "outside.go")
				require.NoError(
					t,
					os.WriteFile(target, []byte("package main\nfunc main() {}\n"), 0o600),
				)
				require.NoError(t, os.Remove(filepath.Join(directory, "main.go")))
				require.NoError(t, os.Symlink(target, filepath.Join(directory, "main.go")))
			}

			output, err := binaryKeyFixtureResult(t, directory, overrides...)
			require.Error(t, err, "an incomplete observation must never select a cached executable")
			require.NotContains(t, output, "ksail-bin-v2-")
		})
	}
}

func legacyBinarySourceHash(t *testing.T, directory string) string {
	t.Helper()

	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	var names []string

	require.NoError(
		t,
		filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}

			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}

			if entry.Name() == "go.mod" || entry.Name() == "go.sum" ||
				strings.HasSuffix(path, ".go") {
				names = append(names, path)
			}

			return nil
		}),
	)
	slices.Sort(names)

	digest := sha256.New()

	for _, name := range names {
		relative, err := filepath.Rel(directory, name)
		require.NoError(t, err)
		contents, err := root.ReadFile(relative)
		require.NoError(t, err)
		_, err = fmt.Fprintf(digest, "%x", sha256.Sum256(contents))
		require.NoError(t, err)
	}

	return hex.EncodeToString(digest.Sum(nil))
}

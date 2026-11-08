package ciharness_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Execute the actual action's key step against a small, dependency-free module.
// A compiled embedded asset must invalidate the cached executable.
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

func TestBinaryCacheKeySupportsLogicalCheckoutPath(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)
	link := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, os.Symlink(directory, link))
	require.Equal(t, binaryKeyFixture(t, directory, "PWD="+directory),
		binaryKeyFixture(t, link, "PWD="+link))
}

func TestBinaryCacheKeyTracksTargetAndBundledCLI(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)

	before := binaryKeyFixture(t, directory)
	for _, change := range []string{
		"RUNNER_ARCH=other", "COPILOT_CLI_VERSION=1.0.88", "GOOS=darwin",
		"GOARCH=arm64", "GOFLAGS=-tags=fixture", "ImageVersion=changed", "GOFIPS140=latest",
	} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			require.NotEqual(t, before, binaryKeyFixture(t, directory, change))
		})
	}
}

func TestBinaryCacheKeyTracksLocalReplacements(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)
	replacement := filepath.Join(directory, "dep")
	addBinaryKeyReplacement(t, directory, replacement)
	before := binaryKeyFixture(t, directory)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(replacement, "data.txt"), []byte("new data\n"), 0o600),
	)
	afterEmbed := binaryKeyFixture(t, directory)
	require.NotEqual(t, before, afterEmbed)
	require.NoError(t, os.WriteFile(filepath.Join(replacement, "version.go"),
		[]byte("package dep\nconst Version = 2\n"), 0o600))
	afterSource := binaryKeyFixture(t, directory)
	require.NotEqual(t, afterEmbed, afterSource)
	require.NoError(t, os.WriteFile(filepath.Join(replacement, "go.mod"),
		[]byte("module example.invalid/dep\n\ngo 1.22.0\n"), 0o600))
	require.NotEqual(t, afterSource, binaryKeyFixture(t, directory))
}

func TestBinaryCacheKeyRefusesUntrackedNativeInputs(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"cgo", "assembly"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			directory := newBinaryKeyFixture(t)
			require.NoError(t, os.Mkdir(filepath.Join(directory, "headers"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "headers/value.h"),
				[]byte("#define VALUE 1\n"), 0o600))

			var overrides []string

			if kind == "cgo" {
				require.NoError(t, os.WriteFile(
					filepath.Join(directory, "main.go"),
					[]byte(
						"package main\n/*\n#include \"headers/value.h\"\n*/\nimport \"C\"\nfunc main() { _ = C.VALUE }\n",
					),
					0o600,
				))

				overrides = []string{
					"CGO_ENABLED=1",
					"GOOS=" + runtime.GOOS,
					"GOARCH=" + runtime.GOARCH,
				}
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(directory, "fixture.s"),
					[]byte("#include \"headers/value.h\"\n"), 0o600))
			}

			output, err := binaryKeyFixtureResult(t, directory, overrides...)
			require.Error(
				t,
				err,
				"untracked transitive native inputs must not select an executable",
			)
			require.Contains(t, output, "requires a separate reviewed cache recipe")
			require.NotContains(t, output, "ksail-bin-v2-")
		})
	}
}

func TestBinaryCacheRecipePinsPureGoBuilds(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/cache-ksail-binary/action.yaml")
	for _, name := range []string{
		"📚 Generate embedded chat documentation", "🔑 Compute ksail cache key", "📦 Cache KSail Binary",
	} {
		require.Equal(t, "0", findHarnessStep(t, action.Runs.Steps, name).Env["CGO_ENABLED"],
			"discovery and compilation must use the same pure-Go recipe as CLI releases")
	}
}

func TestBinaryCacheKeyRefusesOutsideReplacement(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)
	addBinaryKeyReplacement(t, directory, t.TempDir())
	output, err := binaryKeyFixtureResult(t, directory)
	require.Error(t, err)
	require.Contains(t, output, "a local replacement escapes the checkout")
	require.NotContains(t, output, "ksail-bin-v2-")
}

func TestBinaryCacheKeyTracksVendoredInputs(t *testing.T) {
	t.Parallel()
	directory := newBinaryKeyFixture(t)
	vendor := filepath.Join(directory, "vendor")
	dependency := filepath.Join(vendor, "example.invalid", "dep")
	require.NoError(t, os.MkdirAll(dependency, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(directory, "go.mod"),
		[]byte(
			"module example.invalid/binary-key\n\ngo 1.23.0\n\nrequire example.invalid/dep v1.0.0\n",
		),
		0o600,
	))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "main.go"),
		[]byte("package main\nimport _ \"example.invalid/dep\"\nfunc main() {}\n"), 0o600))
	manifest := filepath.Join(vendor, "modules.txt")
	require.NoError(t, os.WriteFile(
		manifest,
		[]byte(
			"# example.invalid/dep v1.0.0\n## explicit; go 1.23.0\nexample.invalid/dep\n",
		),
		0o600,
	))
	require.NoError(t, os.WriteFile(filepath.Join(dependency, "dep.go"),
		[]byte("package dep\nconst Value = 1\n"), 0o600))
	before := binaryKeyFixture(t, directory, "GOFLAGS=-mod=vendor")
	require.NoError(t, os.WriteFile(filepath.Join(dependency, "dep.go"),
		[]byte("package dep\nconst Value = 2\n"), 0o600))
	afterSource := binaryKeyFixture(t, directory, "GOFLAGS=-mod=vendor")
	require.NotEqual(t, before, afterSource)
	require.NoError(t, os.WriteFile(
		manifest,
		[]byte(
			"# example.invalid/dep v1.0.0\n## explicit; go 1.23.1\nexample.invalid/dep\n",
		),
		0o600,
	))
	require.NotEqual(t, afterSource, binaryKeyFixture(t, directory, "GOFLAGS=-mod=vendor"))
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
		"CGO_ENABLED=0",
		"GO_VERSION="+strings.TrimPrefix(strings.TrimSpace(string(version)), "go"),
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
	require.Contains(t, output, "custom GOFLAGS")
}

func TestBinaryCacheKeyRefusesIncompleteInputs(t *testing.T) {
	t.Parallel()

	for fault, diagnostic := range map[string]string{
		"missing-embed":     "no matching files found",
		"missing-module":    "read build input go.mod",
		"unresolved-import": "no required module provides package",
		"wrong-toolchain":   "setup-go version does not match",
		"escaping-source":   "path escapes from parent",
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
			require.Contains(t, output, diagnostic)
		})
	}
}

func addBinaryKeyReplacement(t *testing.T, directory, replacement string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(replacement, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"),
		[]byte("module example.invalid/binary-key\n\ngo 1.23.0\n"+
			"\nrequire example.invalid/dep v0.0.0\nreplace example.invalid/dep => "+replacement+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "main.go"),
		[]byte("package main\nimport _ \"example.invalid/dep\"\nfunc main() {}\n"), 0o600))

	for name, content := range map[string]string{
		"go.mod":   "module example.invalid/dep\n\ngo 1.23.0\n",
		"dep.go":   "package dep\nimport _ \"embed\"\n//go:embed data.txt\nvar Data string\n",
		"data.txt": "data\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(replacement, name), []byte(content), 0o600))
	}
}

package ciharness_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestCalicoBaselineRecipeSelectsMigrationCoverage(t *testing.T) {
	t.Parallel()
	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	filter := findHarnessStep(t, workflow.Jobs["changes"].Steps, "🔍 Filter paths")

	var filters map[string][]string

	require.NoError(t, yaml.Unmarshal([]byte(stringValue(filter.With["filters"])), &filters))
	assert.True(
		t,
		contractFilterMatches(
			t,
			filters["system-test"],
			".github/scripts/build-calico-migration-baseline.sh",
		),
	)
}

// A PR writer or an inexact restore must never seed a shared migration baseline.
func TestCalicoBaselineCacheUsesExactMainBackedEntry(t *testing.T) {
	t.Parallel()
	job := readCIWorkflow(t, ".github/workflows/ci.yaml").Jobs["build-calico-baseline"]
	key := findHarnessStep(t, job.Steps, "🔑 Identify Calico baseline cache")
	checkout := findHarnessStep(t, job.Steps, "📄 Checkout Calico 3.32.2 baseline")
	assert.Equal(t, stringValue(checkout.With["ref"]), key.Env["BASELINE_REF"])
	assert.Equal(
		t,
		"${{ hashFiles('.github/scripts/build-calico-migration-baseline.sh') }}",
		key.Env["RECIPE_HASH"],
	)

	restore := findHarnessStep(t, job.Steps, "♻️ Restore Calico baseline binary")
	assert.Contains(t, restore.Uses, "actions/cache/restore@")
	assert.Equal(t, "${{ steps.calico-cache-key.outputs.key }}", restore.With["key"])
	assert.Equal(t, "${{ runner.temp }}/calico-legacy-bin/ksail", restore.With["path"])
	assert.NotContains(t, restore.With, "restore-keys")
	assert.Empty(t, restore.If)

	save := findHarnessStep(t, job.Steps, "♻️ Save main Calico baseline binary")
	assert.Contains(t, save.Uses, "actions/cache/save@")
	assert.Equal(t, "${{ steps.calico-cache-key.outputs.key }}", save.With["key"])
	assert.Equal(t, restore.With["path"], save.With["path"])
	assert.Equal(
		t,
		"github.event_name == 'push' && "+
			"github.ref == format('refs/heads/{0}', github.event.repository.default_branch) && "+
			"steps.calico-cache.outputs.cache-hit != 'true'",
		save.If,
	)
	assert.Contains(t, job.If, "github.event_name == 'push'")
	assert.Contains(t, job.If, "github.event.repository.default_branch")
}

func TestCalicoBaselineCacheHitPreservesProof(t *testing.T) {
	t.Parallel()
	job := readCIWorkflow(t, ".github/workflows/ci.yaml").Jobs["build-calico-baseline"]
	build := findHarnessStep(t, job.Steps, "🏗️ Build Calico migration baseline")
	assert.Contains(t, build.Run, "bash .github/scripts/build-calico-migration-baseline.sh")
	validator := findHarnessStep(t, job.Steps, "🔍 Validate Calico baseline binary")
	checkout := findHarnessStep(t, job.Steps, "📄 Checkout Calico 3.32.2 baseline")
	assert.Equal(t, stringValue(checkout.With["ref"]), validator.Env["BASELINE_REF"])

	for _, name := range []string{"🧹 Free disk space", "♻️ Restore Go modules", "🏗️ Build Calico migration baseline"} {
		assert.Equal(
			t,
			"steps.calico-cache.outputs.cache-hit != 'true'",
			findHarnessStep(t, job.Steps, name).If,
			name,
		)
	}

	for _, name := range []string{
		"🔍 Verify immutable Calico source",
		"🔍 Validate Calico baseline binary",
		"📤 Publish Calico migration baseline",
	} {
		assert.Empty(t, findHarnessStep(t, job.Steps, name).If, name)
	}

	assert.Less(
		t,
		harnessStepIndex(t, job.Steps, "🔍 Validate Calico baseline binary"),
		harnessStepIndex(t, job.Steps, "♻️ Save main Calico baseline binary"),
	)
	assert.Less(
		t,
		harnessStepIndex(t, job.Steps, "🔍 Validate Calico baseline binary"),
		harnessStepIndex(t, job.Steps, "📤 Publish Calico migration baseline"),
	)
	gate := findHarnessStep(
		t,
		readCIWorkflow(t, ".github/workflows/ci.yaml").Jobs["require-checks-in-pr"].Steps,
		"🔍 Require the selected Calico baseline",
	)
	assert.Contains(t, gate.If, "github.event_name == 'push'")
}

func TestCalicoBaselineCacheKeyBindsRecipeAndTarget(t *testing.T) {
	t.Parallel()
	step := findHarnessStep(
		t,
		readCIWorkflow(t, ".github/workflows/ci.yaml").Jobs["build-calico-baseline"].Steps,
		"🔑 Identify Calico baseline cache",
	)
	base := []string{
		"BASELINE_REF=66ba54d8de006cac38575c3697c863f453fc0f1b",
		"RECIPE_HASH=recipe-a",
		"ImageOS=fixture-os",
		"ImageVersion=fixture-version",
		"GOARCH=amd64",
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	output := filepath.Join(dir, "output")
	_, err = runBinaryArtifactStep(t, step, append(base, "GITHUB_OUTPUT="+output)...)
	require.NoError(t, err)
	first, err := root.ReadFile("output")
	require.NoError(t, err)
	assert.Contains(t, string(first), "key=calico-baseline-bin-v1-")
	assert.Contains(t, string(first), "66ba54d8de006cac38575c3697c863f453fc0f1b-recipe-a")

	for _, change := range []string{
		"BASELINE_REF=other-source", "RECIPE_HASH=recipe-b",
		"ImageVersion=other-image", "GOARCH=arm64",
	} {
		name, _, _ := strings.Cut(change, "=")
		changed := filepath.Join(dir, name)
		env := append(append([]string{}, base...), change, "GITHUB_OUTPUT="+changed)
		_, runErr := runBinaryArtifactStep(t, step, env...)
		require.NoError(t, runErr)

		contents, readErr := root.ReadFile(name)
		require.NoError(t, readErr)
		assert.NotEqual(t, string(first), string(contents))
	}

	for _, field := range []string{"go-version=", "goos=", "goarch=", "cgo-enabled="} {
		assert.Contains(t, string(first), field)
	}
}

// Exercise the actual shell validator against real compiler metadata, not a
// mocked go-version response. Copying a different source's binary must fail.
func TestCalicoBaselineRejectsInvalidCachedBinary(t *testing.T) {
	t.Parallel()
	step := findHarnessStep(
		t,
		readCIWorkflow(t, ".github/workflows/ci.yaml").Jobs["build-calico-baseline"].Steps,
		"🔍 Validate Calico baseline binary",
	)
	binaries, baseEnv, digest := calicoFixtureBinaries(t)

	for _, testCase := range []struct {
		name string
		env  string
	}{
		{name: "valid"},
		{name: "wrong-source", env: "BASELINE_REF=0000000000000000000000000000000000000000"},
		{name: "wrong-toolchain", env: "GO_VERSION=go0.0.0"},
		{name: "wrong-os", env: "TARGET_GOOS=other-os"},
		{name: "wrong-arch", env: "TARGET_GOARCH=other-arch"},
		{name: "wrong-cgo", env: "CGO_ENABLED=other-mode"},
		{name: "missing"},
		{name: "empty"},
		{name: "not-go"},
		{name: "symlink"},
		{name: "dirty"},
		{name: "untrimmed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			path := calicoFixtureBinary(t, testCase.name, binaries)
			dir := t.TempDir()
			root, err := os.OpenRoot(dir)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, root.Close()) })

			env := append([]string{}, baseEnv...)

			env = append(
				env,
				"BASELINE_BINARY="+path,
				"GITHUB_OUTPUT="+filepath.Join(dir, "outputs"),
			)
			if testCase.env != "" {
				env = append(env, testCase.env)
			}

			out, err := runBinaryArtifactStep(t, step, env...)
			if testCase.name != "valid" {
				require.Error(t, err, out)

				_, statErr := root.Stat("outputs")
				assert.ErrorIs(t, statErr, os.ErrNotExist, "invalid baseline must emit no digest")

				return
			}

			require.NoError(t, err, out)
			contents, err := root.ReadFile("outputs")
			require.NoError(t, err)
			assert.Equal(t, "binary-sha256="+digest+"\n", string(contents))
		})
	}
}

func calicoFixtureSource(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "calico-legacy-source")
	require.NoError(t, os.Mkdir(source, 0o750))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(source, "go.mod"),
			[]byte("module github.com/devantler-tech/ksail/v7\n\ngo 1.26.0\n"),
			0o600,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(source, "main.go"),
			[]byte("package main\nfunc main() {}\n"),
			0o600,
		),
	)

	calicoFixtureCommand(t, source, exec.CommandContext(t.Context(), "git", "init", "--quiet"))
	calicoFixtureCommand(
		t,
		source,
		exec.CommandContext(t.Context(), "git", "add", "go.mod", "main.go"),
	)
	calicoFixtureCommand(t, source, exec.CommandContext(t.Context(),
		"git",
		"-c",
		"user.name=CI Fixture",
		"-c",
		"user.email=ci@example.invalid",
		"-c",
		"core.hooksPath=/dev/null",
		"-c",
		"commit.gpgsign=false",
		"commit",
		"--quiet",
		"-m",
		"test: baseline fixture",
	))
	ref := calicoFixtureCommand(
		t,
		source,
		exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD"),
	)

	return dir, source, ref
}

func calicoFixtureBinaries(t *testing.T) (map[string]string, []string, string) {
	t.Helper()
	dir, source, ref := calicoFixtureSource(t)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(dir, "build.sh"),
			readRepoFile(t, ".github/scripts/build-calico-migration-baseline.sh"),
			0o600,
		),
	)
	build := exec.CommandContext(t.Context(), "bash", "build.sh")

	build.Env = append(os.Environ(), "RUNNER_TEMP="+dir)
	calicoFixtureCommand(t, dir, build)
	calicoFixtureVariants(t, source)

	binaries := map[string]string{
		"valid":     filepath.Join(dir, "calico-legacy-bin", "ksail"),
		"untrimmed": filepath.Join(dir, "untrimmed"),
		"dirty":     filepath.Join(dir, "dirty"),
	}
	settings := strings.Fields(
		calicoFixtureCommand(
			t,
			source,
			exec.CommandContext(
				t.Context(),
				"go",
				"env",
				"GOVERSION",
				"GOOS",
				"GOARCH",
				"CGO_ENABLED",
			),
		),
	)
	require.Len(t, settings, 4)
	env := []string{
		"BASELINE_REF=" + ref,
		"GO_VERSION=" + settings[0],
		"TARGET_GOOS=" + settings[1],
		"TARGET_GOARCH=" + settings[2],
		"CGO_ENABLED=" + settings[3],
	}
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	contents, err := root.ReadFile("calico-legacy-bin/ksail")
	require.NoError(t, err)

	return binaries, env, fmt.Sprintf("%x", sha256.Sum256(contents))
}

func calicoFixtureVariants(t *testing.T, source string) {
	t.Helper()
	calicoFixtureCommand(
		t,
		source,
		exec.CommandContext(
			t.Context(),
			"go",
			"build",
			"-p=1",
			"-ldflags=-s -w",
			"-o",
			"../untrimmed",
			".",
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(source, "main.go"),
			[]byte("package main\nfunc main() {}\n// dirty fixture\n"),
			0o600,
		),
	)
	calicoFixtureCommand(
		t,
		source,
		exec.CommandContext(
			t.Context(),
			"go",
			"build",
			"-p=1",
			"-trimpath",
			"-ldflags=-s -w",
			"-o",
			"../dirty",
			".",
		),
	)
}

func calicoFixtureCommand(t *testing.T, dir string, cmd *exec.Cmd) string {
	t.Helper()

	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	return strings.TrimSpace(string(out))
}

func calicoFixtureBinary(t *testing.T, name string, binaries map[string]string) string {
	t.Helper()

	if path, found := binaries[name]; found {
		return path
	}

	path := filepath.Join(t.TempDir(), "baseline")

	switch name {
	case "empty":
		require.NoError(t, os.WriteFile(path, nil, 0o600))
	case "not-go":
		require.NoError(t, os.WriteFile(path, []byte("not a Go executable"), 0o600))
	case "symlink":
		require.NoError(t, os.Symlink(binaries["valid"], path))
	case "missing":
	default:
		return binaries["valid"]
	}

	return path
}

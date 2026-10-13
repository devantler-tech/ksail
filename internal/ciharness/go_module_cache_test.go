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
	"gopkg.in/yaml.v3"
)

// Each repaired caller must execute the same published action that these path
// controls exercise. Removing a restore or editing an unrepinned candidate
// would otherwise leave this consumer adoption untested.
func TestGoModuleRestoreConsumerBindings(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/actions/restore-go-modules/action.yaml")
	require.Equal(t, "0cc2a584876350f4b355373c4a290e4a59b9cc0e0657acdb2a14e101e4d02f53",
		fmt.Sprintf("%x", sha256.Sum256(contents)))

	for path, jobs := range map[string][]string{
		"ci.yaml": {
			"build-artifact", "generate", "home-isolation", "operator-chart-e2e",
			"warm-helm-cache", "warm-mirror-cache", "test-release-config",
			"release-graph-audit", "local-recovery-trial",
		},
		"copilot-setup-steps.yml":       {"copilot-setup-steps"},
		"system-test-eks.yaml":          {"build-artifact"},
		"system-test-hetzner.yaml":      {"build-artifact", "warm-helm-cache"},
		"system-test-omni.yaml":         {"build-artifact", "warm-helm-cache"},
		"update-hetzner-talos-iso.yaml": {"update"},
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			workflow := readCIWorkflow(t, filepath.Join(".github", "workflows", path))
			for _, job := range jobs {
				steps := workflow.Jobs[job].Steps
				restore := findHarnessStep(t, steps, "♻️ Restore Go modules")
				assert.Equal(t,
					"devantler-tech/ksail/.github/actions/restore-go-modules@"+
						"46fdd72dff3ccc24a494508fde43426c7e65a0e3", restore.Uses)
				assert.Less(t,
					harnessStepIndex(t, steps, "⚙️ Setup Go"),
					harnessStepIndex(t, steps, restore.Name),
					"%s restores must follow the configured Go toolchain", job)
			}
		})
	}
}

// An omitted setup-go cache input saves both modules and compilation outputs
// on a miss. That implicit writer can exhaust the budget even when the shared
// validator correctly separates its main-backed module and test-mode caches.
func TestGoSetupNeverImplicitlySavesCaches(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob("../../.github/workflows/*")
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	for _, path := range paths {
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			continue
		}

		var workflow struct {
			Jobs map[string]struct {
				Steps []harnessStep `yaml:"steps"`
			} `yaml:"jobs"`
		}
		require.NoError(t, yaml.Unmarshal(
			readRepoFile(t, filepath.Join(".github", "workflows", filepath.Base(path))), &workflow,
		))

		for job, definition := range workflow.Jobs {
			for _, step := range definition.Steps {
				if strings.HasPrefix(step.Uses, "actions/setup-go@") {
					assert.Equal(t, "false", fmt.Sprint(step.With["cache"]),
						"%s/%s must not save a combined Go archive", filepath.Base(path), job)
				}
			}
		}
	}

	actions, err := filepath.Glob("../../.github/actions/*/action.*")
	require.NoError(t, err)

	for _, path := range actions {
		action := readCompositeAction(
			t,
			filepath.Join(
				".github",
				"actions",
				filepath.Base(filepath.Dir(path)),
				filepath.Base(path),
			),
		)
		for _, step := range action.Runs.Steps {
			if strings.HasPrefix(step.Uses, "actions/setup-go@") {
				assert.Equal(t, "false", fmt.Sprint(step.With["cache"]),
					"%s must not save a combined Go archive", path)
			}
		}
	}
}

func TestGoModuleRestoreOnly(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/restore-go-modules/action.yaml")
	restore := findHarnessStep(t, action.Runs.Steps, "Restore Go module cache")
	assert.True(t, strings.HasPrefix(restore.Uses, "actions/cache/restore@"))
	assert.Equal(t, "${{ steps.module-path.outputs.path }}", stringValue(restore.With["path"]))
	assert.Equal(
		t,
		"go-mod-v1-${{ runner.os }}-${{ runner.arch }}-.-${{ hashFiles('go.sum') }}",
		stringValue(restore.With["key"]),
	)
	assert.Equal(t,
		"${{ github.ref != format('refs/heads/{0}', github.event.repository.default_branch) && "+
			"format('go-mod-v1-{0}-{1}-.-', runner.os, runner.arch) || '' }}",
		stringValue(restore.With["restore-keys"]),
	)

	for _, step := range action.Runs.Steps {
		assert.False(
			t,
			strings.HasPrefix(step.Uses, "actions/cache/save@"),
			"a consumer must not add another module writer",
		)
		assert.False(
			t,
			strings.HasPrefix(step.Uses, "actions/cache@"),
			"the combined action adds a post-job save",
		)
	}
}

func TestGoModuleRestorePathMetadata(t *testing.T) {
	t.Parallel()

	action := readCompositeAction(t, ".github/actions/restore-go-modules/action.yaml")
	identify := findHarnessStep(t, action.Runs.Steps, "Identify Go module cache")

	for _, test := range []struct {
		name, path string
		valid      bool
	}{
		{"absolute path with spaces", "/tmp/module cache", true},
		{"relative path", "relative/modules", false},
		{"multiline path", "/tmp/modules\ninjected=value", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory, err := os.OpenRoot(t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, directory.Close()) })

			outputPath := filepath.Join(directory.Name(), "outputs")
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Stdin = strings.NewReader(identify.Run)

			command.Env = append(os.Environ(), "GOMODCACHE="+test.path, "GITHUB_OUTPUT="+outputPath)

			output, err := command.CombinedOutput()
			if !test.valid {
				require.Error(t, err, "%s", output)

				_, statErr := directory.Stat("outputs")
				assert.ErrorIs(
					t,
					statErr,
					os.ErrNotExist,
					"invalid metadata must not reach the restore action",
				)

				return
			}

			require.NoError(t, err, "%s", output)

			contents, err := directory.ReadFile("outputs")
			require.NoError(t, err)
			assert.Equal(t, "path="+test.path+"\n", string(contents))
		})
	}
}

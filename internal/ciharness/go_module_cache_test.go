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
	require.Equal(t, "bfa9a94ba8de19ad4f8918a7220548b7af6ad2f751f7a9a131bffda3a06be329",
		fmt.Sprintf("%x", sha256.Sum256(contents)))

	for path, jobs := range map[string][]string{
		"ci.yaml": {
			"build-artifact", "generate", "home-isolation", "operator-chart-e2e",
			// Mirror warming is now a binary-only consumer, guarded by
			// TestKSailBinaryArtifactMirrorWarmerConsumer instead.
			"warm-helm-cache", "test-release-config",
			"release-graph-audit", "local-recovery-trial",
			// Module-resolving checks that set up Go without a build cache (#8053).
			"eks-smoke-scripts", "verify-vulnerability-identities",
			"verify-dependency-test-graph", "verify-dependency-contracts",
			"verify-todos-workflow-contract",
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
				assert.Equal(t, ksailActionPin(t, "restore-go-modules"), restore.Uses)
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
				// Cache policy depends only on these fields. Other workflow
				// fields may legally contain expressions, including timeouts.
				Steps []struct {
					Uses string         `yaml:"uses"`
					With map[string]any `yaml:"with"`
				} `yaml:"steps"`
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

// A restore that misses the cache, such as on a pull request that changes go.sum, must not let one
// dropped Go proxy stream fail the job (#8053). The action downloads whatever the restore did not
// supply through the shared bounded retry, read from its own pinned checkout, so every consumer
// recovers a transient download failure while a later build or test failure still stops the job.
func TestGoModuleRestoreDownloadsThroughBoundedRetry(t *testing.T) {
	t.Parallel()
	requireTestExecutable(t, "bash")

	const actionPath = ".github/actions/restore-go-modules"

	steps := readCompositeAction(t, actionPath+"/action.yaml").Runs.Steps
	download := findHarnessStep(t, steps, "Download Go modules")

	assert.Equal(t,
		`bash "$GITHUB_ACTION_PATH/../../scripts/download-go-modules.sh"`,
		strings.TrimSpace(download.Run),
		"consumers must run the audited retry from the action's own pinned checkout")
	assert.Empty(t, download.If, "every consumer must download through the retry")
	assert.False(t, download.ContinueOnError, "an exhausted download must fail the job")
	assert.Less(t,
		harnessStepIndex(t, steps, "Restore Go module cache"),
		harnessStepIndex(t, steps, download.Name),
		"download only what the restored archive did not supply")
	assert.Equal(t, len(steps)-1, harnessStepIndex(t, steps, download.Name),
		"the consumer's own steps must start with every module present")

	for _, scenario := range []moduleDownloadScenario{
		{name: "cache_warm", downloads: 1, operations: 1},
		{name: "two_proxy_blips", failures: 2, downloads: 3, operations: 1, backoff: "5\n10\n"},
		{name: "exhausted", failures: 3, downloads: 3, backoff: "5\n10\n"},
		{name: "downstream_error", operationExit: 37, downloads: 1, operations: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			runModuleDownloadScenario(t, download, "go build", actionPath, scenario)
		})
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

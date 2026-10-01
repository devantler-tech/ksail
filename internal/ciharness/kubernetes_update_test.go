package ciharness_test

import (
	"errors"
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

const versionStub = `#!/usr/bin/env bash
set -euo pipefail
version=$(cat "$FIXTURE_VERSION")
case "$1" in
  version) printf '{"serverVersion":{"gitVersion":"%s"}}\n' "$version" ;;
  get)
    cat <<EOF
{"items":[{"status":{"nodeInfo":{"kubeletVersion":"${FIXTURE_NODE_VERSION:-$version}"},
"conditions":[{"type":"Ready","status":"${FIXTURE_READY:-True}"}]}}]}
EOF
    ;;
  *) exit 99 ;;
esac
`

const updateStub = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FIXTURE_CALLS"
if [[ " $* " == *" --output json "* ]]; then
  printf '{"totalChanges":0}\n'
  echo 'No changes detected' >&2
  if [[ "${FIXTURE_REPEAT_UPGRADE:-false}" == true ]]; then
    echo 'Kubernetes upgrade path: v1.36.2 → v1.37.1 (1 step(s))' >&2
  fi
else
  printf '%s' "${FIXTURE_OBSERVED:-v1.37.1}" > "$FIXTURE_VERSION"
  echo 'Kubernetes upgraded to pinned version v1.37.1'
fi
`

func runVersionStep(t *testing.T, stepName string, env map[string]string) (string, string, error) {
	t.Helper()
	action := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, stepName)
	dir := t.TempDir()
	versionFile := filepath.Join(dir, "version")
	outputFile := filepath.Join(dir, "outputs")
	callsFile := filepath.Join(dir, "calls")
	require.NoError(t, os.WriteFile(versionFile, []byte("v1.36.2"), 0o600))
	writeExecutableStub(t, filepath.Join(dir, "kubectl"), versionStub)
	writeExecutableStub(t, filepath.Join(dir, "ksail"), updateStub)
	writeExecutableStub(t, filepath.Join(dir, "timeout"), "#!/usr/bin/env bash\nshift 2\nexec \"$@\"\n")

	command := exec.CommandContext(t.Context(), "bash")
	command.Stdin = strings.NewReader(step.Run)
	command.Dir = filepath.Join("..", "..")
	command.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"DISTRIBUTION=Talos", "PROVIDER=Docker", "ARGS=", "GHCR_USER=", "GHCR_TOKEN=", "ARTIFACT_TAG=fixture",
		"UPGRADE_FROM=v1.36.2", "UPGRADE_TO=v1.37.1",
		"GITHUB_OUTPUT="+outputFile, "SYSTEM_TEST_LOG_DIR="+dir,
		"FIXTURE_VERSION="+versionFile, "FIXTURE_CALLS="+callsFile,
	)
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	fixtureRoot, openErr := os.OpenRoot(dir)
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, fixtureRoot.Close()) })
	outputs := readOptionalVersionFixture(t, fixtureRoot, "outputs")
	calls := readOptionalVersionFixture(t, fixtureRoot, "calls")

	return string(output) + string(outputs), string(calls), err
}

func readOptionalVersionFixture(t *testing.T, root *os.Root, name string) []byte {
	t.Helper()

	contents, err := root.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil // Failed steps can legitimately leave no output or call record.
	}
	require.NoError(t, err)

	return contents
}

func TestSystemTestTalosNoopUsesRunningVersion(t *testing.T) {
	t.Parallel()
	output, _, err := runVersionStep(t, "🔖 Resolve Kubernetes version pin for update", nil)
	require.NoError(t, err, output)
	assert.Contains(t, output, "flag=--kubernetes-version v1.36.2")
}

func TestSystemTestTalosUpgradeObservesTargetAndRepeatsNoop(t *testing.T) {
	t.Parallel()
	output, calls, err := runVersionStep(t, "🧪 Talos Kubernetes upgrade — known version path", nil)
	require.NoError(t, err, output)
	assert.Equal(t, 2, strings.Count(calls, "cluster update"), calls)
	for line := range strings.SplitSeq(strings.TrimSpace(calls), "\n") {
		assert.Contains(t, line, "--kubernetes-version v1.37.1")
	}
	assert.Contains(t, calls, "--output json")
	assert.Contains(t, output, "v1.36.2 → v1.37.1")
}

func TestSystemTestTalosUpgradeRejectsWrongLiveState(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "server_unchanged", env: map[string]string{"FIXTURE_OBSERVED": "v1.36.2"}, want: "server version"},
		{name: "lagging_node", env: map[string]string{"FIXTURE_NODE_VERSION": "v1.36.2"}, want: "node versions or readiness"},
		{name: "node_not_ready", env: map[string]string{"FIXTURE_READY": "False"}, want: "node versions or readiness"},
		{
			name: "repeat_upgrades", env: map[string]string{"FIXTURE_REPEAT_UPGRADE": "true"},
			want: "repeated update performed a Kubernetes upgrade",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			output, _, err := runVersionStep(t, "🧪 Talos Kubernetes upgrade — known version path", testCase.env)
			require.Error(t, err)
			assert.Contains(t, output, testCase.want)
		})
	}
}

func TestSystemTestTalosUpgradeFixtureRequiresBothVersions(t *testing.T) {
	t.Parallel()
	for _, versions := range []struct{ from, to string }{
		{from: "v1.36.2"}, {to: "v1.37.1"}, {from: "v1.36.2;true", to: "v1.37.1"},
	} {
		t.Run(fmt.Sprintf("%s_%s", versions.from, versions.to), func(t *testing.T) {
			t.Parallel()
			output, _, err := runVersionStep(t, "🔧 Resolve GHCR credentials in args", map[string]string{
				"UPGRADE_FROM": versions.from, "UPGRADE_TO": versions.to,
			})
			require.Error(t, err, output)
		})
	}
}

func TestSystemTestTalosUpgradeMatrixWiresKnownVersions(t *testing.T) {
	t.Parallel()

	var workflow struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []map[string]any `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
			Steps []harnessStep `yaml:"steps"`
		} `yaml:"jobs"`
	}

	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yaml"), &workflow))

	var trialEntries int
	for _, job := range workflow.Jobs {
		for _, entry := range job.Strategy.Matrix.Include {
			if entry["kubernetes-upgrade-from"] == nil && entry["kubernetes-upgrade-to"] == nil {
				continue
			}

			trialEntries++
			assert.Equal(t, "Talos", entry["distribution"])
			assert.Equal(t, "Docker", entry["provider"])
			assert.Equal(t, true, entry["init"])
			assert.Equal(t, "", entry["args"])
			assert.Equal(t, "v1.36.2", entry["kubernetes-upgrade-from"])
			assert.Equal(t, "v1.37.1", entry["kubernetes-upgrade-to"])

			var wired bool
			for _, step := range job.Steps {
				if step.Uses != "./.github/actions/ksail-system-test" {
					continue
				}

				wired = true
				for _, input := range []string{"kubernetes-upgrade-from", "kubernetes-upgrade-to"} {
					assert.Equal(t, "${{ matrix."+input+" || '' }}", step.With[input])
				}
			}
			assert.True(t, wired, "the matrix must pass its known versions to the system test")
		}
	}
	assert.Equal(t, 1, trialEntries, "run one known upgrade path, separate from ordinary no-op legs")
}

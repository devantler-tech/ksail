package ciharness_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCaskHandoffProbeRejectsUnsafeDispatches executes the actual pre-checkout guard.
func TestCaskHandoffProbeRejectsUnsafeDispatches(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		override string
		accepted bool
	}{
		{"main dispatch", "", true},
		{"other event", "GITHUB_EVENT_NAME=push", false},
		{"branch dispatch", "GITHUB_REF=refs/heads/codex/probe", false},
		{"tag dispatch", "GITHUB_REF=refs/tags/v7.202.77", false},
		{"missing confirmation", "CONFIRM_READ_ONLY=", false},
		{"malformed version", "RELEASE_TAG=latest", false},
		{"zero PR", "CLI_PR=0", false},
		{"negative PR", "DESKTOP_PR=-1", false},
		{"multiline PR", "CLI_PR=2019\n42", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, err := runCaskProbeGuard(t, testCase.override)
			if testCase.accepted {
				require.NoError(t, err, output)
			} else {
				require.Error(t, err, output)
			}
		})
	}
}

// TestCaskHandoffProbeShellPayloadRemainsInert rejects shell syntax as input data.
func TestCaskHandoffProbeShellPayloadRemainsInert(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "injected")
	output, err := runCaskProbeGuard(t, "RELEASE_TAG=v7.202.77; touch "+marker)
	require.Error(t, err, output)
	assert.NoFileExists(t, marker)
}

// runCaskProbeGuard executes the reviewed workflow guard with one controlled input override.
func runCaskProbeGuard(t *testing.T, override string) (string, error) {
	t.Helper()

	job := readCIWorkflow(t, ".github/workflows/verify-cask-handoff.yaml").Jobs["verify"]
	require.NotEmpty(t, job.Steps)
	require.Equal(t, "✅ Validate read-only cask verification", job.Steps[0].Name,
		"ref and input validation must run before checkout")

	command := exec.CommandContext( //nolint:gosec // Executes the repository-owned admission guard.
		t.Context(), "bash", "-e", "-o", "pipefail", "-c", job.Steps[0].Run,
	)

	command.Env = append(os.Environ(), "GITHUB_EVENT_NAME=workflow_dispatch",
		"GITHUB_REF=refs/heads/main", "CONFIRM_READ_ONLY=verify published casks",
		"RELEASE_TAG=v7.202.77", "CLI_PR=2019", "DESKTOP_PR=2018")
	if override != "" {
		command.Env = append(command.Env, override)
	}

	output, err := command.CombinedOutput()

	return string(output), err
}

// TestCaskHandoffProbeHasNoAutomaticOrWriteAuthority pins the observation boundary.
func TestCaskHandoffProbeHasNoAutomaticOrWriteAuthority(t *testing.T) {
	t.Parallel()

	var definition struct {
		On          map[string]any    `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
	}
	require.NoError(t, yaml.Unmarshal(
		readRepoFile(t, ".github/workflows/verify-cask-handoff.yaml"), &definition,
	))
	require.Len(t, definition.On, 1)
	assert.Contains(t, definition.On, "workflow_dispatch")
	assert.Equal(t, map[string]string{"contents": "read"}, definition.Permissions)

	workflow := readCIWorkflow(t, ".github/workflows/verify-cask-handoff.yaml")
	require.Len(t, workflow.Jobs, 1)

	for _, job := range workflow.Jobs {
		assert.Empty(t, job.Permissions, "jobs must inherit the read-only ceiling")
		assert.Positive(t, job.TimeoutMinutes)
		assert.LessOrEqual(t, job.TimeoutMinutes, 15)
		assert.False(t, job.ContinueOnError)
	}
}

// TestCaskHandoffProbeReceiptRequiresConsistentObservations exercises the real
// consumer body against controlled release/finder snapshots, including drift.
func TestCaskHandoffProbeReceiptRequiresConsistentObservations(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		scenario, failure string
	}{
		{"stable", ""},
		{"different-pr", "requested named delivery"},
		{"main-drift", "Cask main snapshot changed"},
		{"final-main-drift", "Cask main snapshot changed"},
		{"asset-drift", "Release assets differ"},
		{"final-asset-drift", "Release identity or publication changed"},
		{"release-replaced", "Release identity or publication changed"},
		{"publication-drift", "Release identity or publication changed"},
		{"checkout-mismatch", "Observer checkout does not match"},
		{"missing-release-id", "Select a published"},
		{"missing-pr-number", "Named PR identity is incomplete"},
	} {
		t.Run(testCase.scenario, func(t *testing.T) {
			t.Parallel()

			root, output, err := runCaskProbeObservation(t, testCase.scenario)
			receiptPath := filepath.Join(root, "cask-handoff-verification", "receipt.json")

			if testCase.failure != "" {
				require.Error(t, err, output)
				assert.Contains(t, output, testCase.failure)
				assert.NoFileExists(t, receiptPath)

				return
			}

			require.NoError(t, err, output)
			assertCaskProbeReceipt(t, root)
		})
	}
}

// assertCaskProbeReceipt requires a complete receipt with both independently supplied identities.
func assertCaskProbeReceipt(t *testing.T, root string) {
	t.Helper()

	files, openErr := os.OpenRoot(root)
	require.NoError(t, openErr)

	receiptBytes, readErr := files.ReadFile("cask-handoff-verification/receipt.json")
	require.NoError(t, files.Close())
	require.NoError(t, readErr)

	var receipt map[string]any

	decoder := json.NewDecoder(bytes.NewReader(receiptBytes))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&receipt))
	assert.Equal(
		t,
		"merged identities, current-main blobs and published asset digests",
		receipt["proof_scope"],
	)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", receipt["observer_commit"])
	assert.Equal(t, json.Number("7"), receipt["release_id"])
	assert.Equal(t, "v7.202.77", receipt["release_tag"])
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", receipt["tap_main"])
	casks, ok := receipt["casks"].([]any)
	require.True(t, ok)
	require.Len(t, casks, 2)
	cli, cliOK := casks[0].(map[string]any)
	desktop, desktopOK := casks[1].(map[string]any)

	require.True(t, cliOK)
	require.True(t, desktopOK)
	assert.Equal(t, json.Number("2019"), cli["pr"])
	assert.Equal(t, json.Number("2018"), desktop["pr"])
	assert.Equal(t, "Casks/ksail.rb", cli["path"])
	assert.Equal(t, "Casks/ksail-desktop.rb", desktop["path"])

	for _, cask := range []map[string]any{cli, desktop} {
		assert.Equal(t, "cccccccccccccccccccccccccccccccccccccccc", cask["head"])
		assert.Equal(t, "dddddddddddddddddddddddddddddddddddddddd", cask["merge"])
		assert.Equal(t, "blob-fixture", cask["blob"])
		assert.Equal(t, []any{map[string]any{"name": "fixture.zip", "digest": "sha256:fixture"}},
			cask["release_assets"])
	}
}

// runCaskProbeObservation isolates the workflow's observation body from real GitHub.
// Finder validation has its own integration fixtures; these snapshots target joins.
func runCaskProbeObservation(t *testing.T, scenario string) (string, string, error) {
	t.Helper()

	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	scripts := filepath.Join(root, ".github", "scripts")

	require.NoError(t, os.MkdirAll(bin, 0o700))
	require.NoError(t, os.MkdirAll(scripts, 0o700))
	writeExecutableStub(t, filepath.Join(bin, "gh"), caskProbeGHFixture)
	writeExecutableStub(
		t,
		filepath.Join(bin, "git"),
		"#!/usr/bin/env bash\n[[ \"$PROBE_SCENARIO\" != checkout-mismatch ]] || { echo mismatch; exit 0; }\n"+
			"printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\\n'\n",
	)
	writeExecutableStub(
		t,
		filepath.Join(scripts, "find-merged-cask-pr-handoff.sh"),
		caskProbeFinderFixture,
	)

	steps := readCIWorkflow(t, ".github/workflows/verify-cask-handoff.yaml").Jobs["verify"].Steps
	observer := findHarnessStep(t, steps, "🔎 Verify named merged deliveries")
	command := exec.CommandContext( //nolint:gosec // Executes the repository-owned observation body.
		t.Context(),
		"bash",
		"-e",
		"-o",
		"pipefail",
		"-c",
		observer.Run,
	)
	command.Dir = root
	command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PROBE_SCENARIO="+scenario, "PROBE_STATE="+root,
		"GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"RUNNER_TEMP="+root, "GITHUB_STEP_SUMMARY="+filepath.Join(root, "summary"),
		"RELEASE_TAG=v7.202.77", "CLI_PR=2019", "DESKTOP_PR=2018")
	output, err := command.CombinedOutput()

	return root, string(output), err
}

const caskProbeGHFixture = `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'api repos/devantler-tech/ksail/releases/tags/v7.202.77')
    count=0
    [[ ! -f "$PROBE_STATE/release-read" ]] || count="$(<"$PROBE_STATE/release-read")"
    printf '%s\n' "$((count + 1))" > "$PROBE_STATE/release-read"
    id=7
    [[ "$PROBE_SCENARIO" != missing-release-id ]] || id=null
    draft=false
    digest=sha256:fixture
    if [[ "$count" -gt 0 ]]; then
      [[ "$PROBE_SCENARIO" != release-replaced ]] || id=8
      [[ "$PROBE_SCENARIO" != publication-drift ]] || draft=true
      [[ "$PROBE_SCENARIO" != final-asset-drift ]] || digest=sha256:changed
    fi
    jq -n --argjson id "$id" --argjson draft "$draft" --arg digest "$digest" '
      {id:$id,tag_name:"v7.202.77",draft:$draft,prerelease:false,
       published_at:"2026-10-15T05:15:00Z",
       assets:[{name:"fixture.zip",digest:$digest}]}'
    ;;
  'api repos/devantler-tech/homebrew-tap/git/ref/heads/main --jq .object.sha')
    if [[ "$PROBE_SCENARIO" == final-main-drift && -f "$PROBE_STATE/main-read" ]]; then
      printf 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n'
      exit 0
    fi
    touch "$PROBE_STATE/main-read"
    printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
    ;;
  *) printf 'unexpected API fixture request\n' >&2; exit 1 ;;
esac
`

const caskProbeFinderFixture = `#!/usr/bin/env bash
set -euo pipefail
name='' output='' pr=''
while (($#)); do
  case "$1" in
    --cask-name) name="$2"; shift 2 ;;
    --output-dir) output="$2"; shift 2 ;;
    --pr) pr="$2"; shift 2 ;;
    --tap|--source-repo|--tag) shift 2 ;;
    *) exit 1 ;;
  esac
done
main=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
digest=sha256:fixture
if [[ "$name" == ksail-desktop ]]; then
  [[ "$PROBE_SCENARIO" != main-drift ]] || main=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  [[ "$PROBE_SCENARIO" != asset-drift ]] || digest=sha256:changed
fi
recorded_pr="$pr"
[[ "$PROBE_SCENARIO" != missing-pr-number ]] || recorded_pr=null
jq -n --argjson pr "$recorded_pr" --arg name "$name" --arg main "$main" --arg digest "$digest" '
  {pr:{number:$pr,head:{sha:"cccccccccccccccccccccccccccccccccccccccc"},
       merge_commit_sha:"dddddddddddddddddddddddddddddddddddddddd"},
   mainRef:{sha:$main},mainFile:{path:("Casks/"+$name+".rb"),sha:"blob-fixture"},
   releaseAssets:[{name:"fixture.zip",digest:$digest}]}
' > "$output/merged-$pr.json"
[[ "$PROBE_SCENARIO" != different-pr ]] || { printf '999\n'; exit 0; }
printf '%s\n' "$pr"
`

package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	splitImageActionPath = ".github/actions/ksail-test-image-export-import/action.yaml"
	splitImageStepName   = "🧪 Export and import images held on different nodes"
	splitImageCluster    = "nested-kind-routing"
	splitImageDigestA    = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	splitImageDigestB    = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	splitImageDigestBad  = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

type splitImageScenario struct {
	name, mode, cluster, onCI, wantError string
}

// The split-image check must tell a correct multi-node export from a wrong one (#8251): with
// content shared between the nodes, an export that dropped a worker-only image still passed. Run
// the step exactly as CI does against a simulated two-node cluster: a correct export and import
// pass, and every way an archive, an import or the fixture itself can be wrong fails.
func TestSplitImageExportStepDetectsWrongExports(t *testing.T) {
	t.Parallel()

	// The check reads the archive index with jq, so a missing jq must fail this test, never skip it.
	_, err := exec.LookPath("jq")
	require.NoError(t, err, "the split-image check needs jq")

	step := findHarnessStep(
		t,
		readCompositeAction(t, splitImageActionPath).Runs.Steps,
		splitImageStepName,
	)
	assert.Equal(t, "inputs.split-image-cluster-name != ''", step.If)
	assert.Equal(t, "${{ inputs.split-image-cluster-name }}", step.Env["IMAGE_EXPORT_CLUSTER"])
	assert.False(t, step.ContinueOnError, "a wrong export must fail the system test")

	for _, scenario := range []splitImageScenario{
		{name: "correct export and import"},
		{name: "archive stores its index under a dot path", mode: "dot-paths"},
		{name: "selected export drops the worker image", mode: "drop-worker", wantError: "exactly the requested images"},
		{name: "full export drops the worker image", mode: "drop-worker-full", wantError: "full export left out"},
		{name: "selected export adds an image", mode: "extra", wantError: "exactly the requested images"},
		{name: "import restores different content", mode: "wrong-content", wantError: "original content"},
		{name: "import restores nothing", mode: "import-noop", wantError: "original content"},
		{name: "unreadable node inventory", mode: "inventory-error", wantError: "Cannot read the image inventory"},
		{name: "fixture content on both nodes", mode: "not-split", wantError: "the fixture is not split"},
		{name: "node labels of another role", mode: "wrong-role", wantError: "is not the control-plane"},
		{name: "another cluster", cluster: "someone-elses-cluster", wantError: "unexpected cluster identity"},
		{name: "outside CI", onCI: "false", wantError: "GitHub-hosted CI runner"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			runSplitImageScenario(t, step, scenario)
		})
	}
}

func runSplitImageScenario(t *testing.T, step harnessStep, scenario splitImageScenario) {
	t.Helper()

	state, runnerTemp := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(state, "nodes"), 0o700))

	output, runErr := splitImageCommand(t, step, scenario, state, runnerTemp).CombinedOutput()
	if scenario.wantError == "" {
		require.NoError(t, runErr, "%s", output)
		assert.Contains(
			t,
			string(output),
			"Restored both node-only images on both nodes from all.tar",
		)
	} else {
		require.Error(t, runErr, "%s", output)
		assert.Contains(t, string(output), scenario.wantError)
	}

	stateRoot, err := os.OpenRoot(state)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stateRoot.Close()) })

	timeoutArgs, err := stateRoot.ReadFile("timeout.args")
	require.NoError(t, err, "CI runs the check under its timeout wrapper")
	assert.True(
		t,
		strings.HasPrefix(string(timeoutArgs), "--kill-after=15s 420s bash "),
		"%s",
		timeoutArgs,
	)

	assertSplitImageCleanedUp(t, stateRoot, runnerTemp)
}

// splitImageCommand runs the step's own CI command line against the simulated cluster.
func splitImageCommand(
	t *testing.T,
	step harnessStep,
	scenario splitImageScenario,
	state, runnerTemp string,
) *exec.Cmd {
	t.Helper()

	stubs := t.TempDir()
	writeSplitImageStubs(t, stubs)

	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	cluster, onCI := scenario.cluster, scenario.onCI
	if cluster == "" {
		cluster = splitImageCluster
	}

	if onCI == "" {
		onCI = "true"
	}

	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Stdin = strings.NewReader(step.Run)

	command.Env = append(
		os.Environ(),
		"PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_WORKSPACE="+root,
		"GITHUB_ACTIONS="+onCI,
		"RUNNER_ENVIRONMENT=github-hosted",
		"RUNNER_TEMP="+runnerTemp,
		// Keep the stubs' own temporary files inside test-owned storage.
		"TMPDIR="+t.TempDir(),
		"IMAGE_EXPORT_CLUSTER="+cluster,
		"SPLIT_STATE="+state,
		"SPLIT_MODE="+scenario.mode,
		"DIGEST_A="+splitImageDigestA,
		"DIGEST_B="+splitImageDigestB,
		"DIGEST_BAD="+splitImageDigestBad,
	)

	return command
}

// Whatever the outcome, no node keeps fixture content and the staging directory is gone.
func assertSplitImageCleanedUp(t *testing.T, state *os.Root, runnerTemp string) {
	t.Helper()

	nodes, err := os.ReadDir(filepath.Join(state.Name(), "nodes"))
	require.NoError(t, err)

	for _, node := range nodes {
		contents, readErr := state.ReadFile(filepath.Join("nodes", node.Name()))
		require.NoError(t, readErr)

		for _, digest := range []string{splitImageDigestA, splitImageDigestB} {
			assert.NotContains(t, string(contents), digest, "%s keeps fixture content", node.Name())
		}
	}

	staged, err := os.ReadDir(runnerTemp)
	require.NoError(t, err)
	assert.Empty(t, staged, "the staging directory must be removed")
}

// writeSplitImageStubs simulates the runner's Docker daemon, the two Kind nodes' containerd
// image stores and the KSail CLI. A fake digest stands for each fixture's content.
func writeSplitImageStubs(t *testing.T, directory string) {
	t.Helper()
	writeExecutableStub(t, filepath.Join(directory, "timeout"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"$SPLIT_STATE/timeout.args"
shift 2
exec "$@"
`)
	writeExecutableStub(t, filepath.Join(directory, "docker"), splitImageDockerStub)
	writeExecutableStub(t, filepath.Join(directory, "ksail"), splitImageKSailStub)
}

const splitImageDockerStub = `#!/usr/bin/env bash
set -euo pipefail
nodes="$SPLIT_STATE/nodes"
put() { # put <file> <ref> <digest>: store ref, replacing an earlier entry
	touch "$1"
	awk -v ref="$2" '$1 != ref' "$1" >"$1.tmp"
	printf '%s %s\n' "$2" "$3" >>"$1.tmp"
	mv "$1.tmp" "$1"
}
case "$1" in
inspect)
	node="${!#}"
	case "$node" in
	*-control-plane) role=control-plane cluster="${node%-control-plane}" ;;
	*-worker) role=worker cluster="${node%-worker}" ;;
	*) exit 1 ;;
	esac
	[[ "$SPLIT_MODE" != wrong-role ]] || role=worker
	printf '%s %s\n' "$cluster" "$role"
	;;
image)
	case "$2" in
	import)
		cat >/dev/null
		case "$4" in *-a:*) digest="$DIGEST_A" ;; *-b:*) digest="$DIGEST_B" ;; *) exit 1 ;; esac
		put "$SPLIT_STATE/local" "$4" "$digest"
		;;
	save) awk -v ref="$3" '$1 == ref' "$SPLIT_STATE/local" ;;
	rm)
		shift 3
		touch "$SPLIT_STATE/local"
		for ref; do
			awk -v ref="$ref" '$1 != ref' "$SPLIT_STATE/local" >"$SPLIT_STATE/local.tmp"
			mv "$SPLIT_STATE/local.tmp" "$SPLIT_STATE/local"
		done
		;;
	*) exit 64 ;;
	esac
	;;
exec)
	shift
	[[ "$1" != -i ]] || shift
	node="$1"
	[[ "$2 $3 $4" == "ctr --namespace=k8s.io images" ]] || exit 64
	file="$nodes/$node"
	touch "$file"
	case "$5" in
	list)
		[[ "$SPLIT_MODE" != inventory-error || "$node" != *-worker ]] || exit 1
		printf 'REF TYPE DIGEST SIZE PLATFORMS LABELS\n'
		awk '{print $1, "application/vnd.oci.image.manifest.v1+json", $2, "1.0", "KiB", "linux/amd64", "-"}' "$file"
		;;
	import)
		while read -r ref digest; do
			put "$file" "$ref" "$digest"
			if [[ "$SPLIT_MODE" == not-split && "$node" == *-control-plane ]]; then
				put "$nodes/${node%-control-plane}-worker" "$ref" "$digest"
			fi
		done
		;;
	rm) shift 5; for ref; do awk -v ref="$ref" '$1 != ref' "$file" >"$file.tmp"; mv "$file.tmp" "$file"; done ;;
	*) exit 64 ;;
	esac
	;;
*) exit 64 ;;
esac
`

const splitImageKSailStub = `#!/usr/bin/env bash
set -euo pipefail
nodes="$SPLIT_STATE/nodes"
[[ "$1" == workload ]] || exit 64
case "$2" in
export)
	out="$3"
	shift 3
	work=$(mktemp -d)
	cat "$nodes"/* | awk '!seen[$1]++' >"$work/held"
	if [[ "$#" -gt 0 ]]; then
		for arg; do awk -v ref="${arg#--image=}" '$1 == ref' "$work/held"; done >"$work/images"
		[[ "$SPLIT_MODE" != extra ]] || printf 'localhost/unrequested:fixture %s\n' "$DIGEST_BAD" >>"$work/images"
	else
		cp "$work/held" "$work/images"
	fi
	if [[ "$SPLIT_MODE" == drop-worker || ( "$SPLIT_MODE" == drop-worker-full && "$#" -eq 0 ) ]]; then
		awk -v digest="$DIGEST_B" '$2 != digest' "$work/images" >"$work/kept"
		mv "$work/kept" "$work/images"
	fi
	{
		printf '{"schemaVersion":2,"manifests":['
		separator=
		while read -r ref digest; do
			printf '%s{"digest":"%s","annotations":{"io.containerd.image.name":"%s"}}' "$separator" "$digest" "$ref"
			separator=,
		done <"$work/images"
		printf ']}\n'
	} >"$work/index.json"
	if [[ "$SPLIT_MODE" == dot-paths ]]; then
		tar -C "$work" -cf "$out" ./index.json ./images
	else
		tar -C "$work" -cf "$out" index.json images
	fi
	rm -rf "$work"
	;;
import)
	[[ "$SPLIT_MODE" != import-noop ]] || exit 0
	images=$(tar -xOf "$3" images 2>/dev/null || tar -xOf "$3" ./images)
	for file in "$nodes"/*; do
		while read -r ref digest; do
			[[ "$SPLIT_MODE" != wrong-content || "$digest" != "$DIGEST_B" ]] || digest="$DIGEST_BAD"
			awk -v ref="$ref" '$1 != ref' "$file" >"$file.tmp"
			printf '%s %s\nimport-2026-11-12@%s %s\n' "$ref" "$digest" "$digest" "$digest" >>"$file.tmp"
			mv "$file.tmp" "$file"
		done <<<"$images"
	done
	;;
*) exit 64 ;;
esac
`

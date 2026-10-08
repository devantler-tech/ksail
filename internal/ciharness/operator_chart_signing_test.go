package ciharness_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperatorChartPublicationRequiresVerifiedSignature(t *testing.T) {
	t.Parallel()
	jobs := readReleaseJobs(t)
	job := jobs["operator-chart"]
	assert.Equal(t, []string{"goreleaser"}, job.Needs)
	assert.Equal(t, "write", job.Permissions["id-token"])
	assert.False(t, job.ContinueOnError)
	assert.Empty(t, job.If)

	for _, name := range []string{"publish-release", "pages-deploy"} {
		assert.Contains(t, jobs[name].Needs, "operator-chart")
	}

	installed := false
	publications := 0

	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "sigstore/cosign-installer@") {
			assert.Empty(t, step.If)
			assert.False(t, step.ContinueOnError)

			installed = true
		}

		if strings.TrimSpace(step.Run) == "bash .github/scripts/publish-operator-chart.sh" {
			publications++

			assert.True(t, installed, "install Cosign before publishing")
			assert.Empty(t, step.If)
			assert.False(t, step.ContinueOnError)
		}

		assert.NotContains(
			t,
			step.Run,
			"helm push",
			"unsigned publication must not bypass the script",
		)
	}

	assert.Equal(t, 1, publications)
	assert.True(
		t,
		contractFilterMatches(
			t,
			changesFilterPatterns(t, "release-config"),
			".github/scripts/publish-operator-chart.sh",
		),
		"script-only edits must execute their contract tests",
	)
}

const operatorChartRepository = "ghcr.io/devantler-tech/charts/ksail-operator"

type operatorChartPublisherCase struct {
	name, ref, receipt, reason                  string
	packageExit, pushExit, signExit, verifyExit int
	wantCalls                                   int
}

func operatorChartPublisherCases() []operatorChartPublisherCase {
	digest := "sha256:" + strings.Repeat("a", 64)
	receipt := "Pushed: " + operatorChartRepository + ":7.202.4\nDigest: " + digest + "\n"

	return []operatorChartPublisherCase{
		{name: "signed digest", ref: "refs/tags/v7.202.4", receipt: receipt, wantCalls: 4},
		{
			name:    "invalid ref",
			ref:     "refs/heads/main",
			receipt: receipt,
			reason:  "Invalid release ref",
		},
		{
			name: "failed package", ref: "refs/tags/v7.202.4", receipt: receipt,
			packageExit: 1, reason: "package failed", wantCalls: 1,
		},
		{
			name: "failed push", ref: "refs/tags/v7.202.4", receipt: receipt,
			pushExit: 1, reason: "push failed", wantCalls: 2,
		},
		{
			name: "missing digest", ref: "refs/tags/v7.202.4", receipt: "Pushed: " + operatorChartRepository + ":7.202.4\n",
			reason: "Invalid chart push receipt", wantCalls: 2,
		},
		{
			name: "malformed digest", ref: "refs/tags/v7.202.4",
			receipt: "Pushed: " + operatorChartRepository + ":7.202.4\nDigest: sha256:bad\n",
			reason:  "Invalid chart push receipt", wantCalls: 2,
		},
		{
			name: "ambiguous digest", ref: "refs/tags/v7.202.4", receipt: receipt + "Digest: " + digest + "\n",
			reason: "Invalid chart push receipt", wantCalls: 2,
		},
		{
			name: "wrong artifact", ref: "refs/tags/v7.202.4",
			receipt: strings.ReplaceAll(receipt, operatorChartRepository, "ghcr.io/example/other"),
			reason:  "Invalid chart push receipt", wantCalls: 2,
		},
		{
			name: "wrong version", ref: "refs/tags/v7.202.4",
			receipt: strings.ReplaceAll(
				receipt,
				":7.202.4",
				":7.202.3",
			), reason: "Invalid chart push receipt", wantCalls: 2,
		},
		{
			name: "failed signing", ref: "refs/tags/v7.202.4", receipt: receipt,
			signExit: 1, reason: "sign failed", wantCalls: 3,
		},
		{
			name: "unsigned or wrong identity", ref: "refs/tags/v7.202.4", receipt: receipt,
			verifyExit: 1, reason: "verify failed", wantCalls: 4,
		},
	}
}

func operatorChartPublisherCommand(
	t *testing.T, script string, testCase operatorChartPublisherCase,
) (*exec.Cmd, string) {
	t.Helper()
	directory := t.TempDir()
	record := filepath.Join(directory, "calls")
	writeExecutableStub(t, filepath.Join(directory, "helm"), `#!/bin/sh
printf 'helm %s\n' "$*" >>"$CHART_TEST_RECORD"
case "$1" in
  package)
    if [ "$CHART_TEST_PACKAGE_EXIT" -ne 0 ]; then echo 'package failed' >&2; fi
    exit "$CHART_TEST_PACKAGE_EXIT" ;;
  push)
    printf '%s' "$CHART_TEST_RECEIPT"
    if [ "$CHART_TEST_PUSH_EXIT" -ne 0 ]; then echo 'push failed' >&2; fi
    exit "$CHART_TEST_PUSH_EXIT" ;;
  *) exit 99 ;;
esac
`)
	writeExecutableStub(t, filepath.Join(directory, "cosign"), `#!/bin/sh
printf 'cosign %s\n' "$*" >>"$CHART_TEST_RECORD"
case "$1" in
  sign)
    if [ "$CHART_TEST_SIGN_EXIT" -ne 0 ]; then echo 'sign failed' >&2; fi
    exit "$CHART_TEST_SIGN_EXIT" ;;
  verify)
    if [ "$CHART_TEST_VERIFY_EXIT" -ne 0 ]; then echo 'verify failed' >&2; fi
    exit "$CHART_TEST_VERIFY_EXIT" ;;
  *) exit 99 ;;
esac
`)
	//nolint:gosec // Runs a fixed repository-owned publisher with test-owned inputs.
	command := exec.CommandContext(t.Context(), "bash", script)
	command.Dir = directory
	command.Env = append(envWithoutReleaseRef(),
		"PATH="+directory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_REF="+testCase.ref,
		"CHART_TEST_RECORD="+record,
		"CHART_TEST_RECEIPT="+testCase.receipt,
		fmt.Sprintf("CHART_TEST_PACKAGE_EXIT=%d", testCase.packageExit),
		fmt.Sprintf("CHART_TEST_PUSH_EXIT=%d", testCase.pushExit),
		fmt.Sprintf("CHART_TEST_SIGN_EXIT=%d", testCase.signExit),
		fmt.Sprintf("CHART_TEST_VERIFY_EXIT=%d", testCase.verifyExit))

	return command, record
}

func assertOperatorChartPublisherCalls(
	t *testing.T,
	script, record string,
	testCase operatorChartPublisherCase,
) {
	t.Helper()

	calls, readErr := os.ReadFile(record) //nolint:gosec // Test-owned recording file.
	if testCase.wantCalls == 0 {
		assert.ErrorIs(t, readErr, os.ErrNotExist, "invalid input must reach no publisher")

		return
	}

	lines := strings.Split(strings.TrimSpace(string(calls)), "\n")

	require.NoError(t, readErr)
	require.Len(t, lines, testCase.wantCalls)
	assert.Equal(
		t,
		"helm package "+filepath.Join(filepath.Dir(script), "../../charts/ksail-operator")+
			" --version 7.202.4 --app-version 7.202.4",
		lines[0],
	)

	if testCase.wantCalls >= 2 {
		assert.Equal(
			t,
			"helm push ksail-operator-7.202.4.tgz oci://ghcr.io/devantler-tech/charts",
			lines[1],
		)
	}

	digest := "sha256:" + strings.Repeat("a", 64)
	if testCase.wantCalls >= 3 {
		assert.Equal(t, "cosign sign --yes "+operatorChartRepository+"@"+digest, lines[2])
	}

	if testCase.wantCalls == 4 {
		assert.Equal(
			t,
			"cosign verify --certificate-oidc-issuer https://token.actions.githubusercontent.com "+
				"--certificate-identity https://github.com/devantler-tech/ksail/.github/workflows/cd.yaml@"+
				testCase.ref+" "+operatorChartRepository+"@"+digest,
			lines[3],
		)
	}
}

func TestOperatorChartPublisherFailsClosed(t *testing.T) {
	t.Parallel()

	script, err := filepath.Abs("../../.github/scripts/publish-operator-chart.sh")
	require.NoError(t, err)
	require.FileExists(t, script, "the successful publisher must exist before testing rejection")

	for _, testCase := range operatorChartPublisherCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			command, record := operatorChartPublisherCommand(t, script, testCase)

			output, runErr := command.CombinedOutput()
			if testCase.reason == "" {
				require.NoErrorf(t, runErr, "successful publication rejected: %s", output)
			} else {
				require.Errorf(t, runErr, "unsafe publication succeeded: %s", output)
				assert.Contains(t, string(output), testCase.reason)
			}

			assertOperatorChartPublisherCalls(t, script, record, testCase)
		})
	}
}

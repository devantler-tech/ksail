package ciharness_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Run the shipping preflight, with bounded GitHub/Cosign fixtures replacing only network tools.
// Existing publication must be proved before any expensive or mutating release step is admitted.
func TestReleaseReplayEntrypoint(t *testing.T) {
	t.Parallel()
	step := findHarnessStep(
		t,
		readReleaseJobs(t)["validate-release-ref"].Steps,
		"🔎 Resolve release replay",
	)
	require.False(t, step.ContinueOnError)
	require.Empty(t, step.If, "every full release attempt must classify its publication state")

	for _, test := range []struct {
		name, mode, diagnostic string
	}{
		{"new", "build", ""},
		{"published", "verify", ""},
		{"missing-asset", "", "asset set"},
		{"wrong-source", "", "source"},
		{"read-failure", "", "release state"},
		{"invalid-attestation", "", "attestation"},
		{"failed-publisher", "", "publisher jobs"},
		{"unsigned-image", "", "OCI"},
		{"mutable-release", "", "immutable"},
		{"duplicate-release", "", "conflicting release"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := releaseReplayFixture(t)
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Dir = directory
			command.Stdin = strings.NewReader(step.Run)
			command.Env = releaseReplayEnvironment(t, directory, test.name)
			diagnostic, err := command.CombinedOutput()
			root, rootErr := os.OpenRoot(directory)
			require.NoError(t, rootErr)
			t.Cleanup(func() { require.NoError(t, root.Close()) })

			output, readErr := root.ReadFile("output")

			if test.mode == "" {
				require.Error(t, err, "unverified publication must not admit release work")
				require.Contains(t, string(diagnostic), test.diagnostic)
				require.NotContains(t, string(output), "mode=")

				return
			}

			require.NoErrorf(t, err, "preflight failed: %s", diagnostic)
			require.NoError(t, readErr)
			require.Contains(t, string(output), "mode="+test.mode+"\n")

			if test.mode == "verify" {
				require.Contains(t, string(output), "proof=")
			}
		})
	}
}

func releaseReplayEnvironment(t *testing.T, directory, scenario string) []string {
	t.Helper()

	attempt := "2"
	if scenario == "new" {
		attempt = "1"
	}

	return []string{
		"PATH=" + filepath.Join(
			directory,
			"bin",
		) + string(
			os.PathListSeparator,
		) + os.Getenv(
			"PATH",
		),
		"HOME=" + t.TempDir(),
		"LC_ALL=C",
		"REPLAY_FIXTURE=" + directory,
		"REPLAY_CASE=" + scenario,
		"GITHUB_REPOSITORY=devantler-tech/ksail",
		"GITHUB_REF=refs/tags/v1.2.3",
		"GITHUB_SHA=" + strings.Repeat("a", 40),
		"GITHUB_EVENT_NAME=push",
		"GITHUB_RUN_ID=42",
		"GITHUB_RUN_ATTEMPT=" + attempt,
		"GITHUB_OUTPUT=" + filepath.Join(directory, "output"),
	}
}

func releaseReplayFixture(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	for _, path := range []string{".github/scripts", "charts/ksail-operator/crds", "bin"} {
		require.NoError(t, os.MkdirAll(filepath.Join(directory, path), 0o700))
	}

	for _, name := range []string{"validate-release-ref.sh", "resolve-release-replay.sh"} {
		path := filepath.Join(".github", "scripts", name)
		require.NoError(
			t,
			os.WriteFile(filepath.Join(directory, path), readRepoFile(t, path), 0o600),
		)
	}

	installer := []byte("#!/bin/sh\necho fixture\n")
	crd := []byte("kind: CustomResourceDefinition\n")

	require.NoError(t, os.WriteFile(filepath.Join(directory, "install.sh"), installer, 0o600))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(directory, "charts/ksail-operator/crds/ksail.io_clusters.yaml"),
			crd,
			0o600,
		),
	)
	assets := releaseReplayAssets(t, directory, installer, crd)
	releaseReplayJSON(t, directory, "assets.json", assets)
	releaseReplayMetadata(t, directory)
	releaseReplayAttestation(t, directory, assets)
	writeExecutableStub(t, filepath.Join(directory, "bin/gh"), releaseReplayFakeGitHub)
	writeExecutableStub(t, filepath.Join(directory, "bin/cosign"), releaseReplayFakeCosign)

	return directory
}

func releaseReplayMetadata(t *testing.T, directory string) {
	t.Helper()

	releaseReplayJSON(t, directory, "release.json", map[string]any{
		"id": 55, "tag_name": "v1.2.3", "draft": false, "immutable": true,
	})
	releaseReplayJSON(t, directory, "state.json", map[string]any{"data": map[string]any{
		"repository": map[string]any{
			"nameWithOwner": "devantler-tech/ksail",
			"release": map[string]any{
				"databaseId": 55, "tagName": "v1.2.3", "isDraft": false,
			},
		},
	}})
	releaseReplayJSON(t, directory, "ref.json", map[string]any{
		"ref":    "refs/tags/v1.2.3",
		"object": map[string]any{"type": "commit", "sha": strings.Repeat("a", 40)},
	})
	releaseReplayJSON(t, directory, "run.json", map[string]any{
		"id":          42,
		"run_attempt": 1,
		"head_sha":    strings.Repeat("a", 40),
		"head_branch": "v1.2.3",
		"event":       "push",
		"path":        ".github/workflows/cd.yaml",
		"repository":  map[string]any{"full_name": "devantler-tech/ksail"},
		"actor":       map[string]any{"login": "ksail-bot[bot]"},
	})

	jobs := make([]map[string]any, 0, 8)
	for _, name := range []string{
		"🧩 Release CLI", "🧩 Release VSCode Extension", "🧩 Release Desktop App (macOS cask)",
		"🧩 Release Desktop App (ubuntu-24.04, linux, amd64)",
		"🧩 Release Desktop App (windows-latest, windows, amd64, .exe)",
		"⛵ Release Operator Chart", "📄 Build Pages", "📢 Publish Release",
	} {
		jobs = append(
			jobs,
			map[string]any{"name": name, "status": "completed", "conclusion": "success"},
		)
	}

	releaseReplayJSON(
		t,
		directory,
		"jobs.json",
		map[string]any{"jobs": jobs, "total_count": len(jobs)},
	)
}

func releaseReplayAssets(t *testing.T, directory string, installer, crd []byte) []map[string]any {
	t.Helper()

	names := []string{
		"install.sh",
		"ksail.io_clusters.yaml",
		"ksail_1.2.3_checksums.txt",
		"ksail_1.2.3_darwin_arm64.tar.gz",
		"ksail_1.2.3_linux_amd64.tar.gz",
		"ksail_1.2.3_linux_arm64.tar.gz",
		"ksail_1.2.3_windows_amd64.zip",
		"ksail_1.2.3_windows_arm64.zip",
		"KSail_1.2.3_darwin_arm64.zip",
		"ksail-desktop_1.2.3_linux_amd64.tar.gz",
		"ksail-desktop_1.2.3_windows_amd64.zip",
		"ksail_1.2.3.vsix",
	}

	assets := make([]map[string]any, 0, len(names))

	var checksums strings.Builder
	for _, name := range names[3:8] {
		fmt.Fprintf(&checksums, "%s  %s\n", strings.Repeat("b", 64), name)
	}

	require.NoError(
		t,
		os.WriteFile(filepath.Join(directory, "checksums.txt"), []byte(checksums.String()), 0o600),
	)

	for index, name := range names {
		digest := strings.Repeat("b", 64)

		switch name {
		case "install.sh":
			digest = fmt.Sprintf("%x", sha256.Sum256(installer))
		case "ksail.io_clusters.yaml":
			digest = fmt.Sprintf("%x", sha256.Sum256(crd))
		case "ksail_1.2.3_checksums.txt":
			digest = fmt.Sprintf("%x", sha256.Sum256([]byte(checksums.String())))
		}

		assets = append(
			assets,
			map[string]any{"id": index + 1, "name": name, "size": 64, "digest": "sha256:" + digest},
		)
	}

	return assets
}

func releaseReplayAttestation(t *testing.T, directory string, assets []map[string]any) {
	t.Helper()

	subjects := make([]map[string]any, 0, 1+len(assets))

	subjects = append(subjects, map[string]any{
		"uri":    "pkg:github/devantler-tech/ksail@v1.2.3",
		"digest": map[string]any{"sha1": strings.Repeat("a", 40)},
	})
	for _, asset := range assets {
		subjects = append(subjects, map[string]any{
			"name": asset["name"],
			"digest": map[string]any{
				"sha256": strings.TrimPrefix(fmt.Sprint(asset["digest"]), "sha256:"),
			},
		})
	}

	releaseReplayJSON(
		t,
		directory,
		"attestation.json",
		map[string]any{"verificationResult": map[string]any{
			"statement": map[string]any{
				"_type":         "https://in-toto.io/Statement/v1",
				"subject":       subjects,
				"predicateType": "https://in-toto.io/attestation/release/v0.2",
				"predicate": map[string]any{
					"databaseId":   "55",
					"repository":   "devantler-tech/ksail",
					"repositoryId": "737584922",
					"tag":          "v1.2.3",
				},
			},
		}},
	)
}

func releaseReplayJSON(t *testing.T, directory, name string, value any) {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), data, 0o600))
}

const releaseReplayFakeGitHub = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$REPLAY_FIXTURE/github-calls"
if [[ $1 == api && $2 == graphql ]]; then
  case $REPLAY_CASE in
    new) jq '.data.repository.release = null' "$REPLAY_FIXTURE/state.json" ;;
    read-failure) exit 1 ;;
    *) cat "$REPLAY_FIXTURE/state.json" ;;
  esac
elif [[ $1 == release && $2 == verify ]]; then
  [[ $* == *v1.2.3* ]] || exit 91
  if [[ $REPLAY_CASE == invalid-attestation ]]; then exit 1; fi
  cat "$REPLAY_FIXTURE/attestation.json"
else
  case "$*" in
    'api --paginate repos/devantler-tech/ksail/releases?per_page=100')
      if [[ $REPLAY_CASE == duplicate-release ]]; then
        jq -s '. + [{"id":56,"tag_name":"v1.2.3","draft":true,"assets":[]}]' "$REPLAY_FIXTURE/release.json"
      else jq -s '.' "$REPLAY_FIXTURE/release.json"; fi ;;
    'api repos/devantler-tech/ksail/git/ref/tags/v1.2.3')
      if [[ $REPLAY_CASE == wrong-source ]]; then
        jq '.object.sha = "cccccccccccccccccccccccccccccccccccccccc"' "$REPLAY_FIXTURE/ref.json"
      else cat "$REPLAY_FIXTURE/ref.json"; fi ;;
    'api repos/devantler-tech/ksail/releases/55')
      if [[ $REPLAY_CASE == mutable-release ]]; then
        jq '.immutable = false' "$REPLAY_FIXTURE/release.json"
      else cat "$REPLAY_FIXTURE/release.json"; fi ;;
    'api --paginate repos/devantler-tech/ksail/releases/55/assets?per_page=100')
      if [[ $REPLAY_CASE == missing-asset ]]; then
        jq '.[0:-1]' "$REPLAY_FIXTURE/assets.json"
      else cat "$REPLAY_FIXTURE/assets.json"; fi ;;
    'api repos/devantler-tech/ksail/actions/runs/42/attempts/1') cat "$REPLAY_FIXTURE/run.json" ;;
    'api --paginate repos/devantler-tech/ksail/actions/runs/42/attempts/1/jobs?per_page=100')
      if [[ $REPLAY_CASE == failed-publisher ]]; then
        jq '.jobs[0].conclusion = "failure"' "$REPLAY_FIXTURE/jobs.json"
      else cat "$REPLAY_FIXTURE/jobs.json"; fi ;;
    'api repos/devantler-tech/ksail/releases/assets/3 -H Accept: application/octet-stream')
      cat "$REPLAY_FIXTURE/checksums.txt" ;;
    *) printf 'Unexpected GitHub call: %s\n' "$*" >&2; exit 91 ;;
  esac
fi
`

const releaseReplayFakeCosign = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$REPLAY_FIXTURE/cosign-calls"
[[ $* == *"--certificate-github-workflow-sha $GITHUB_SHA"* ]] || exit 91
[[ $* == *"--certificate-oidc-issuer https://token.actions.githubusercontent.com"* ]] || exit 91
[[ $* == *"https://github.com/devantler-tech/ksail/.github/workflows/cd.yaml@refs/tags/v1.2.3"* ]] || exit 91
if [[ $REPLAY_CASE == unsigned-image ]]; then exit 1; fi
digest="sha256:$(printf 'b%.0s' {1..64})"
if [[ $REPLAY_CASE == changed-image ]]; then digest="sha256:$(printf 'c%.0s' {1..64})"; fi
printf '[{"critical":{"image":{"docker-manifest-digest":"%s"}}}]\n' "$digest"
`

func TestReleaseReplayFinalReadback(t *testing.T) {
	t.Parallel()
	jobs := readReleaseJobs(t)
	before := findHarnessStep(t, jobs["validate-release-ref"].Steps, "🔎 Resolve release replay")

	after := findHarnessStep(t, jobs["publish-release"].Steps, "🔎 Recheck immutable release proof")
	for _, scenario := range []string{"published", "changed-image"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			directory := releaseReplayFixture(t)
			values, diagnostic, err := executeReleaseReplay(t, before, directory, "published")
			require.NoErrorf(t, err, "initial verification: %s", diagnostic)

			_, proof, found := strings.Cut(values, "proof=")
			require.True(t, found)

			values, diagnostic, err = executeReleaseReplay(t, after, directory, scenario,
				"REPLAY_EXPECTED="+strings.TrimSpace(proof))
			if scenario == "published" {
				require.NoErrorf(t, err, "unchanged publication: %s", diagnostic)
				require.Contains(t, values, "mode=verify")
			} else {
				require.Error(t, err)
				require.Contains(t, diagnostic, "changed during replay")
				require.NotContains(t, values, "mode=")
			}
		})
	}
}

func executeReleaseReplay(t *testing.T, step harnessStep, directory, scenario string,
	overrides ...string,
) (string, string, error) {
	t.Helper()

	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	err = root.Remove("output")
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
	}

	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
	command.Dir = directory
	command.Stdin = strings.NewReader(step.Run)

	command.Env = append(releaseReplayEnvironment(t, directory, scenario), overrides...)
	diagnostic, runErr := command.CombinedOutput()

	values, readErr := root.ReadFile("output")
	if !errors.Is(readErr, os.ErrNotExist) {
		require.NoError(t, readErr)
	}

	return string(values), string(diagnostic), runErr
}

func releaseReplayDependencies() map[string]string {
	return map[string]string{
		"cask-checkpoint": "validate-release-ref", "goreleaser": "cask-checkpoint",
		"vscode-extension": "validate-release-ref", "desktop-macos": "cask-checkpoint",
		"desktop": "validate-release-ref", "operator-chart": "goreleaser",
		"mcp-publish": "goreleaser", "pages-build": "validate-release-ref",
		"pages-deploy": "goreleaser", "publish-release": "goreleaser",
		"homebrew": "cask-checkpoint", "cleanup-failed-release": "validate-release-ref",
	}
}

// A partial rerun must never carry a stale first-attempt build decision into expensive work.
func TestReleaseReplayJobsRequireFreshAdmission(t *testing.T) {
	t.Parallel()

	jobs := readReleaseJobs(t)
	for name, dependency := range releaseReplayDependencies() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			job := jobs[name]
			step := findHarnessStep(t, job.Steps, "🔒 Verify release admission")
			require.Equal(
				t,
				"devantler-tech/ksail/.github/actions/release-admission@ac2dd2ee401030b2fac81dc985b42fae45b0788b",
				step.Uses,
			)
			require.Empty(t, step.If)
			require.False(t, step.ContinueOnError)

			for input, output := range map[string]string{
				"attempt": "release-attempt", "mode": "release-mode", "proof": "replay-proof",
			} {
				require.Equal(t, "${{ needs."+dependency+".outputs."+output+" }}", step.With[input])
			}
		})
	}
}

func TestReleaseAdmissionBindsAttemptModeAndProof(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/release-admission/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🔒 Verify full-attempt release admission")

	proof := `{"source":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",` +
		`"tag":"v1.2.3","originalRun":42,"releaseId":55,` +
		`"assets":[{},{},{},{},{},{},{},{},{},{},{},{}],"images":{"a":"b","c":"d","e":"f"}}`

	wrongSource := strings.Replace(proof, strings.Repeat("a", 40), strings.Repeat("c", 40), 1)
	for _, test := range []struct {
		name, admitted, mode, receipt, diagnostic string
	}{
		{"new-build", "2", "build", "", ""},
		{"verified", "2", "verify", proof, ""},
		{"stale-attempt", "1", "build", "", "Fresh full-attempt admission"},
		{"missing-mode", "2", "", "", "Fresh release mode"},
		{"missing-proof", "2", "verify", "", "Fresh verified publication proof"},
		{"different-source", "2", "verify", wrongSource, "Fresh verified publication proof"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail")
			command.Stdin = strings.NewReader(step.Run)
			command.Env = []string{
				"PATH=" + os.Getenv("PATH"),
				"GITHUB_RUN_ATTEMPT=2",
				"GITHUB_RUN_ID=42",
				"GITHUB_REF_NAME=v1.2.3",
				"GITHUB_SHA=" + strings.Repeat("a", 40),
				"ADMITTED_ATTEMPT=" + test.admitted,
				"ADMITTED_MODE=" + test.mode,
				"ADMITTED_PROOF=" + test.receipt,
			}

			output, err := command.CombinedOutput()
			if test.diagnostic == "" {
				require.NoErrorf(t, err, "admitted decision failed: %s", output)
			} else {
				require.Error(t, err)
				require.Contains(t, string(output), test.diagnostic)
			}
		})
	}
}

func TestReleaseReplayReusesPublishedGoArtifacts(t *testing.T) {
	t.Parallel()
	jobs := readReleaseJobs(t)
	dependencies := releaseReplayDependencies()

	for _, name := range []string{"goreleaser", "desktop-macos", "desktop", "operator-chart"} {
		job := jobs[name]
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout@") ||
				step.Uses == "devantler-tech/ksail/.github/actions/release-admission@ac2dd2ee401030b2fac81dc985b42fae45b0788b" {
				continue
			}

			mode := "build"
			if step.Name == "♻️ Reuse verified publication" {
				mode = "verify"
			}

			require.Contains(
				t,
				step.If,
				"needs."+dependencies[name]+".outputs.release-mode == '"+mode+"'",
				"%s must not rebuild or overwrite an already verified publication: %s",
				name,
				step.Name,
			)
		}

		require.Contains(
			t,
			findHarnessStep(t, job.Steps, "♻️ Reuse verified publication").If,
			"verify",
		)
	}

	final := findHarnessStep(t, jobs["publish-release"].Steps, "🔎 Recheck immutable release proof")
	require.Equal(t, "bash .github/scripts/resolve-release-replay.sh", final.Run)
	require.Equal(t, "${{ needs.goreleaser.outputs.replay-proof }}", final.Env["REPLAY_EXPECTED"])
	require.Contains(t, final.If, "release-mode == 'verify'")
	pages := findHarnessStep(t, jobs["pages-deploy"].Steps, "🚀 Deploy to GitHub Pages")
	require.Contains(
		t,
		pages.If,
		"cask-publish == 'true'",
		"an older replay must not replace current documentation",
	)
}

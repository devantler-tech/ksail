package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNPMAuditReadOnlyModeDoesNotModifyPackages(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	fix := findHarnessStep(t, action.Runs.Steps, "🔧 Attempt automatic fix")
	directory, environment := npmAuditFixture(t, "changed")
	environment["REPAIR_ALLOWED"] = "false"

	output, diagnostics, err := runNPMAuditStep(
		t,
		fix.Run,
		filepath.Join(directory, "docs"),
		environment,
	)
	require.NoError(t, err, diagnostics)
	assert.Contains(t, output, "changes_made=false\n")
	assert.Empty(t, auditFixtureGit(t, directory, "diff", "--name-only"),
		"a bot or fork audit must leave its manifests and unrelated files untouched")
}

func TestNPMAuditRepairProducesOnlyPackagePatch(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	fix := findHarnessStep(t, action.Runs.Steps, "🔧 Attempt automatic fix")
	directory, environment := npmAuditFixture(t, "changed")
	environment["REPAIR_ALLOWED"] = "true"

	output, diagnostics, err := runNPMAuditStep(
		t,
		fix.Run,
		filepath.Join(directory, "docs"),
		environment,
	)
	require.NoError(t, err, diagnostics)
	assert.Contains(t, output, "changes_made=true\n")

	patch, err := os.ReadFile(environment["PATCH_PATH"])
	require.NoError(t, err, "a successful fix must prepare a patch for the signed writer")
	assert.Contains(t, string(patch), "a/docs/package.json b/docs/package.json")
	assert.Contains(t, string(patch), "a/docs/package-lock.json b/docs/package-lock.json")
	assert.NotContains(
		t,
		string(patch),
		"unrelated.txt",
		"only package manifests belong in this repair",
	)
	assert.Equal(t, "1\n", auditFixtureGit(t, directory, "rev-list", "--count", "HEAD"),
		"the auditor prepares patches; it never commits")
}

func TestNPMAuditUnchangedOrFailedFixHasNoPatch(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	fix := findHarnessStep(t, action.Runs.Steps, "🔧 Attempt automatic fix")

	for _, mode := range []string{"unchanged", "failed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			directory, environment := npmAuditFixture(t, mode)
			environment["REPAIR_ALLOWED"] = "true"
			output, diagnostics, err := runNPMAuditStep(
				t,
				fix.Run,
				filepath.Join(directory, "docs"),
				environment,
			)

			if mode == "failed" {
				require.Error(t, err, diagnostics)
			} else {
				require.NoError(t, err, diagnostics)
			}

			assert.NotContains(t, output, "changes_made=true\n")

			patch, readErr := os.ReadFile(environment["PATCH_PATH"])
			if !os.IsNotExist(readErr) {
				require.NoError(t, readErr)
				assert.Empty(t, patch, "an unchanged or failed fix must not publish a repair")
			}
		})
	}
}

func TestNPMAuditDoesNotReportPreparedRepairAsSourcePass(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	result := findHarnessStep(t, action.Runs.Steps, "📊 Determine final result")

	for _, test := range []struct {
		name, audit, changed, reaudit, want string
	}{
		{"clean source", "success", "", "", "true"},
		{"read-only finding", "failure", "", "", "false"},
		{"prepared repair", "failure", "true", "success", "false"},
		{"incomplete repair", "failure", "true", "failure", "false"},
		{"failed audit", "cancelled", "", "", "false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			output, diagnostics, err := runNPMAuditStep(
				t,
				result.Run,
				t.TempDir(),
				map[string]string{
					"AUDIT_OUTCOME":    test.audit,
					"FIX_CHANGES_MADE": test.changed,
					"REAUDIT_OUTCOME":  test.reaudit,
				},
			)
			require.NoError(t, err, diagnostics)
			assert.Contains(t, output, "passed="+test.want+"\n")
		})
	}
}

func TestNPMAuditSourceFailureFailsProtectedBranch(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	fail := findHarnessStep(t, action.Runs.Steps, "❌ Fail if vulnerabilities remain")
	directory := t.TempDir()
	writeExecutableStub(t, filepath.Join(directory, "npm"), "#!/bin/sh\nexit 0\n")
	_, diagnostics, err := runNPMAuditStep(t, fail.Run, directory, map[string]string{
		"PATH": directory + string(os.PathListSeparator) + os.Getenv("PATH"),
	})
	require.Error(t, err, "a repaired local workspace must not hide a failed source audit")
	assert.Contains(t, diagnostics, "source npm audit failed")
}

func TestProtectedCorrectionRequiresVerifiedSignatures(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	check := findHarnessStep(t, workflow.Jobs["auto-commit"].Steps,
		"🔏 Verify protected-branch correction signatures")

	for _, verified := range []string{"true", "false", ""} {
		t.Run("verified="+verified, func(t *testing.T) {
			t.Parallel()

			_, diagnostics, err := runNPMAuditStep(t, check.Run, t.TempDir(),
				map[string]string{"COMMITS_VERIFIED": verified})
			if verified == "true" {
				require.NoError(t, err, diagnostics)
			} else {
				require.Error(t, err, "false or missing signature evidence must fail closed")
				assert.Contains(t, diagnostics, "commits are not verified")
			}
		})
	}
}

func TestNPMAuditPublishesOnlyReauditedRepairs(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	upload := findHarnessStep(t, action.Runs.Steps, "📤 Upload proposed repair")
	assert.Equal(
		t,
		"steps.fix.outputs.changes_made == 'true' && steps.reaudit.outcome == 'success'",
		upload.If,
	)

	reauditIndex, uploadIndex := -1, -1

	for index, step := range action.Runs.Steps {
		if step.ID == "reaudit" {
			reauditIndex = index
		}

		if step.ID == "upload" {
			uploadIndex = index
		}
	}

	assert.GreaterOrEqual(t, reauditIndex, 0)
	assert.Greater(
		t,
		uploadIndex,
		reauditIndex,
		"unverified repairs must not reach the signed writer",
	)
}

func TestNPMAuditDelegatesWritesToSignedCorrectionJob(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	for _, step := range action.Runs.Steps {
		assert.NotContains(
			t,
			step.Uses,
			"create-github-app-token",
			"audit jobs must not mint write credentials",
		)
		assert.NotContains(
			t,
			step.Uses,
			"git-auto-commit",
			"the auditor must not take ownership of a branch",
		)
	}

	checkout := findHarnessStep(t, action.Runs.Steps, "📄 Checkout")
	assert.Equal(t, false, checkout.With["persist-credentials"])
	assert.Equal(t, "false", action.Outputs["changes-committed"].Value)

	workflow := readCIWorkflow(t, ".github/workflows/ci.yaml")
	for _, name := range []string{"audit-docs", "audit-vsce"} {
		job, found := workflow.Jobs[name]
		require.True(t, found)
		assert.Equal(t, "read", job.Permissions["contents"])
		audit := findHarnessStep(t, job.Steps, "🔍 NPM Audit and Fix")
		assert.NotContains(t, audit.With, "client-id")
		assert.NotContains(t, audit.With, "app-private-key")
		assert.True(t, strings.HasSuffix(stringValue(audit.With["patch-artifact-name"]), "-patch"))
	}

	writer, found := workflow.Jobs["auto-commit"]
	require.True(t, found)
	token := findHarnessStep(t, writer.Steps, "🔑 Generate GitHub App Token")
	assert.Equal(t, "write", token.With["permission-contents"])
	assert.Equal(t, "write", token.With["permission-pull-requests"])
	assert.Contains(t, writer.Needs, "audit-docs")
	assert.Contains(t, writer.Needs, "audit-vsce")
	assert.Contains(t, writer.If, "github.event_name != 'merge_group'")
	assert.Contains(
		t,
		writer.If,
		"github.event.pull_request.head.repo.full_name == github.repository",
	)
	assert.Contains(t, writer.If, "github.event.pull_request.user.login != 'dependabot[bot]'")
	assert.Contains(t, writer.If, "github.event.pull_request.user.login != 'renovate[bot]'")
	protected := findHarnessStep(
		t,
		writer.Steps,
		"📤 Open PR for generated changes (protected branch)",
	)
	assert.Equal(
		t,
		true,
		protected.With["sign-commits"],
		"protected-branch repairs require verified signatures",
	)
}

func npmAuditFixture(t *testing.T, mode string) (string, map[string]string) {
	t.Helper()
	requireTestExecutable(t, "git")

	directory := t.TempDir()
	seedNPMAuditRepository(t, directory)
	bin := filepath.Join(t.TempDir(), "bin")
	require.NoError(t, os.Mkdir(bin, 0o700))
	writeExecutableStub(t, filepath.Join(bin, "npm"), `#!/bin/sh
set -eu
[ "$*" = "audit fix --force" ] || exit 97
case "$FIX_MODE" in
  changed)
    printf 'repaired\n' > package.json
    printf 'repaired lock\n' > package-lock.json
    printf 'unrelated change\n' > unrelated.txt
    ;;
  unchanged) ;;
  failed)
    printf 'incomplete repair\n' > package.json
    exit 1
    ;;
  *) exit 98 ;;
esac
`)

	return directory, map[string]string{
		"PATH":       bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"FIX_MODE":   mode,
		"PATCH_PATH": filepath.Join(t.TempDir(), "npm.patch"),
	}
}

func seedNPMAuditRepository(t *testing.T, directory string) {
	t.Helper()

	packageDirectory := filepath.Join(directory, "docs")
	require.NoError(t, os.Mkdir(packageDirectory, 0o700))

	for _, name := range []string{"package.json", "package-lock.json", "unrelated.txt"} {
		require.NoError(
			t,
			os.WriteFile(filepath.Join(packageDirectory, name), []byte("original\n"), 0o600),
		)
	}

	auditFixtureGit(t, directory, "init", "--quiet")
	auditFixtureGit(
		t,
		directory,
		"add",
		"docs/package.json",
		"docs/package-lock.json",
		"docs/unrelated.txt",
	)
	auditFixtureGit(
		t,
		directory,
		"-c",
		"user.name=Fixture",
		"-c",
		"user.email=fixture@example.invalid",
		"-c",
		"commit.gpgsign=false",
		"commit",
		"--quiet",
		"-m",
		"fixture",
	)
}

func auditFixtureGit(t *testing.T, directory string, args ...string) string {
	t.Helper()

	//nolint:gosec // Git arguments and repository paths are test-owned fixtures.
	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = directory

	command.Env = append(os.Environ(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=/dev/null")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, output)

	return string(output)
}

func TestNPMAuditRepairPolicy(t *testing.T) {
	t.Parallel()

	action := readNPMAuditAction(t)
	policy := findHarnessStep(t, action.Runs.Steps, "🛡️ Select audit mode")

	const (
		repo        = "devantler-tech/ksail"
		pullRequest = "pull_request"
		prRef       = "refs/pull/1/merge"
		bot         = "dependabot[bot]"
		maintainer  = "devantler"
		mainRef     = "refs/heads/main"
		queueRef    = "refs/heads/gh-readonly-queue/main/pr-1"
		dispatch    = "workflow_dispatch"
		renovate    = "renovate[bot]"
	)

	tests := []struct {
		name, event, ref, author, headRepo, repository, actor string
		wantRepair                                            string
	}{
		{"dependabot", pullRequest, prRef, bot, repo, repo, bot, "false"},
		{"maintainer reruns bot", pullRequest, prRef, bot, repo, repo, maintainer, "false"},
		{"renovate", pullRequest, prRef, renovate, repo, repo, renovate, "false"},
		{"same repository", pullRequest, prRef, maintainer, repo, repo, maintainer, "true"},
		{"fork", pullRequest, prRef, maintainer, "example/ksail", repo, maintainer, "false"},
		{"missing author", pullRequest, prRef, "", repo, repo, maintainer, "false"},
		{"missing repository", pullRequest, prRef, maintainer, "", "", maintainer, "false"},
		{"protected branch", "push", mainRef, "", "", repo, maintainer, "true"},
		{"feature branch", "push", "refs/heads/topic", "", "", repo, maintainer, "false"},
		{"merge group", "merge_group", queueRef, "", "", repo, maintainer, "false"},
		{"manual dispatch", dispatch, mainRef, "", "", repo, maintainer, "false"},
		{"unknown event", "", mainRef, "", "", repo, maintainer, "false"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			output, diagnostics, err := runNPMAuditStep(
				t,
				policy.Run,
				t.TempDir(),
				map[string]string{
					"AUDIT_EVENT": test.event, "AUDIT_REF": test.ref,
					"AUDIT_PR_AUTHOR": test.author, "AUDIT_HEAD_REPO": test.headRepo,
					"AUDIT_REPOSITORY": test.repository, "GITHUB_ACTOR": test.actor,
				},
			)
			require.NoError(t, err, diagnostics)
			assert.Equal(t, "repair-allowed="+test.wantRepair+"\n", output)
		})
	}
}

func readNPMAuditAction(t *testing.T) compositeAction {
	t.Helper()

	var action compositeAction
	require.NoError(
		t,
		yaml.Unmarshal(readRepoFile(t, ".github/actions/npm-audit-and-fix/action.yaml"), &action),
	)

	return action
}

func runNPMAuditStep(
	t *testing.T,
	script, directory string,
	environment map[string]string,
) (string, string, error) {
	t.Helper()
	requireTestExecutable(t, "bash")

	outputPath := filepath.Join(t.TempDir(), "outputs")
	require.NoError(t, os.WriteFile(outputPath, nil, 0o600))

	//nolint:gosec // The inline script is repository-owned workflow content.
	command := exec.CommandContext(t.Context(), "bash", "-e", "-o", "pipefail", "-c", script)
	command.Dir = directory

	command.Env = append(os.Environ(), "GITHUB_OUTPUT="+outputPath,
		"GITHUB_STEP_SUMMARY="+filepath.Join(t.TempDir(), "summary"))

	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}

	diagnostics, err := command.CombinedOutput()
	output, readErr := os.ReadFile(outputPath) //nolint:gosec // Test-owned output path.
	require.NoError(t, readErr)

	return strings.TrimSpace(string(output)) + "\n", string(diagnostics), err
}

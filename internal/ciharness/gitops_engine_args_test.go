package ciharness_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkloadGitOpsReconciliationAcceptsBothFlagForms(t *testing.T) {
	t.Parallel()
	action := readCompositeAction(t, ".github/actions/ksail-test-workload/action.yaml")
	step := findHarnessStep(t, action.Runs.Steps, "🧪 ksail workload push and reconcile")
	// Evaluate the literal contains operands from the real action condition.
	condition := regexp.MustCompile(`contains\(format\(' \{0\} ', inputs.args\), '([^']+)'\)`)
	operands := condition.FindAllStringSubmatch(step.If, -1)
	require.NotEmpty(t, operands)

	for _, scenario := range []struct{ args, engine string }{
		{"--gitops-engine Flux", "Flux"},
		{"--gitops-engine=Flux", "Flux"},
		{"--name fixture --gitops-engine ArgoCD --workers 1", "ArgoCD"},
		{"--name fixture --gitops-engine=ArgoCD --workers 1", "ArgoCD"},
		{"--name fixture", ""},
		{"--gitops-engine None", ""},
		{"--gitops-engine=FluxOther", ""},
		{"--gitops-engine ArgoCDOther", ""},
	} {
		t.Run(scenario.args, func(t *testing.T) {
			t.Parallel()

			selected := false
			for _, operand := range operands {
				selected = selected || strings.Contains(" "+scenario.args+" ", operand[1])
			}

			require.Equal(t, scenario.engine != "", selected)

			if selected {
				runGitOpsReconciliationFixture(t, step.Run, scenario.args, scenario.engine)
			}
		})
	}
}

func runGitOpsReconciliationFixture(t *testing.T, script, args, engine string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(bin, 0o700))

	assertionDir := filepath.Join(dir, ".github", "scripts")
	require.NoError(t, os.MkdirAll(assertionDir, 0o700))
	writeExecutableStub(
		t,
		filepath.Join(bin, "ksail"),
		"#!/bin/bash\nprintf '%s\\n' \"$*\" >> \"$COMMAND_LOG\"\n",
	)
	writeExecutableStub(t, filepath.Join(bin, "yq"), "#!/bin/bash\nexit 0\n")
	writeExecutableStub(
		t,
		filepath.Join(assertionDir, "assert-gitops-deployed.sh"),
		"#!/bin/bash\nset -eu\n[[ -f \"$GITOPS_PATH/$2.yaml\" ]]\nprintf '%s %s\\n' \"$1\" \"$2\" > \"$ASSERTION_LOG\"\n",
	)
	commandLog := filepath.Join(dir, "commands")
	assertionLog := filepath.Join(dir, "assertion")
	//nolint:gosec // Executes the fixed repository-owned action with test-owned command stubs.
	command := exec.CommandContext(t.Context(), "bash", "-c", script)
	command.Dir = dir
	command.Env = append(
		os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"ARGS="+args,
		"DISTRIBUTION=Vanilla",
		"GITHUB_WORKSPACE="+dir,
		"GITOPS_PATH="+filepath.Join(
			dir,
			"gitops",
		),
		"COMMAND_LOG="+commandLog,
		"ASSERTION_LOG="+assertionLog,
	)
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "%s", output)
	//nolint:gosec // Reads this test's private command receipt.
	commands, err := os.ReadFile(
		commandLog,
	)
	require.NoError(t, err)
	require.Contains(t, string(commands), "workload push --path ")
	require.Contains(t, string(commands), "workload reconcile\n")

	//nolint:gosec // Reads this test's private assertion receipt.
	assertion, err := os.ReadFile(
		assertionLog,
	)
	require.NoError(t, err)
	require.Equal(t, engine+" ksail-gitops-system-test\n", string(assertion))
}

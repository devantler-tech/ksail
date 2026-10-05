package ciharness_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var (
	errGoMaintenanceIdentity = errors.New(
		"go validation must remain the reviewed bare reusable call",
	)
	errGoMaintenanceAdmission = errors.New(
		"go validation admission must retain main and the same-repository allowlist PR boundary",
	)
	errGoMaintenanceEnvelope = errors.New(
		"go validation must preserve its existing credential envelope",
	)
	errGoMaintenanceInputs = errors.New(
		"go maintenance requires a boolean opt-in and explicit boolean signed-fix opt-out",
	)
)

// The additional caller must preserve its narrow PR admission and sole-writer
// opt-out when maintenance is enabled on main. The callee's maintenance jobs
// hold contents:read; the existing caller envelope also serves its other checks.
func validateGoMaintenanceCaller(contents []byte) error {
	var workflow ciWorkflow

	err := yaml.Unmarshal(contents, &workflow)
	if err != nil {
		return fmt.Errorf("decode workflow: %w", err)
	}

	var caller todosWorkflow

	err = yaml.Unmarshal(contents, &caller)
	if err != nil {
		return fmt.Errorf("caller inputs or secrets must remain explicit mappings: %w", err)
	}

	err = validateGoMaintenanceIdentity(workflow, caller)
	if err != nil {
		return err
	}

	job := workflow.Jobs["ci-go"]

	const admission = "(github.event_name == 'push' && github.ref == 'refs/heads/main') || " +
		"(github.event_name == 'pull_request' && " +
		"github.event.pull_request.head.repo.full_name == github.repository && " +
		"needs.changes.outputs.govuln-allowlist == 'true')"
	if normalizeGoAdmission(job.If) != normalizeGoAdmission(admission) {
		return errGoMaintenanceAdmission
	}

	if !reflect.DeepEqual(job.Permissions, map[string]string{
		"contents": "write", "issues": "write", "pull-requests": "write", "code-quality": "write",
	}) || !reflect.DeepEqual(caller.Jobs["ci-go"].Secrets, map[string]string{
		"APP_PRIVATE_KEY": "${{ secrets.APP_PRIVATE_KEY }}",
	}) {
		return errGoMaintenanceEnvelope
	}

	if !reflect.DeepEqual(caller.Jobs["ci-go"].With, map[string]any{
		"maintenance-default-branch": true, "apply-signed-fixes": false,
	}) {
		return errGoMaintenanceInputs
	}

	return nil
}

func validateGoMaintenanceIdentity(workflow ciWorkflow, caller todosWorkflow) error {
	job, found := workflow.Jobs["ci-go"]
	if !found || job.Uses != "devantler-tech/.github/.github/workflows/validate-go-project.yaml@"+
		"0600006235510307a04efebcac1ac1f363f5f862" || len(job.Steps) != 0 ||
		caller.Jobs["ci-go"].RunsOn != "" || !reflect.DeepEqual(job.Needs, []string{"changes"}) {
		return errGoMaintenanceIdentity
	}

	return nil
}

// Normalize formatting outside literals without changing branch/event values.
func normalizeGoAdmission(expression string) string {
	var result strings.Builder

	quoted := false

	for _, char := range expression {
		if char == '\'' {
			quoted = !quoted
		}

		if quoted || !unicode.IsSpace(char) {
			result.WriteRune(char)
		}
	}

	return result.String()
}

func TestGoMaintenanceCallerUsesReadOnlyPilot(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateGoMaintenanceCaller(readRepoFile(t, ".github/workflows/ci.yaml")))
}

type goMaintenanceMutation struct {
	name   string
	mutate func(*testing.T, map[string]any)
	want   string
}

func goMaintenanceAdmissionMutations() []goMaintenanceMutation {
	return []goMaintenanceMutation{
		{"main ref removed", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["if"] = strings.Replace(
				goMaintenanceString(t, job, "if"),
				" && github.ref == 'refs/heads/main'",
				"",
				1,
			)
		}, "admission"},
		{"fork restriction removed", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["if"] = strings.Replace(goMaintenanceString(t, job, "if"),
				"&& github.event.pull_request.head.repo.full_name == github.repository", "", 1)
		}, "admission"},
		{"PR selection broadened", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["if"] = strings.Replace(
				goMaintenanceString(t, job, "if"),
				"govuln-allowlist",
				"code",
				1,
			)
		}, "admission"},
		{"dispatch replaces push", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["if"] = strings.Replace(
				goMaintenanceString(t, job, "if"),
				"'push'",
				"'workflow_dispatch'",
				1,
			)
		}, "admission"},
		{"unconditional admission", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["if"] = goMaintenanceString(t, job, "if") + " || true"
		}, "admission"},
		{"literal branch whitespace changed", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["if"] = strings.Replace(
				goMaintenanceString(t, job, "if"),
				"refs/heads/main",
				"refs/heads/ main",
				1,
			)
		}, "admission"},
	}
}

func goMaintenanceInputMutations() []goMaintenanceMutation {
	return []goMaintenanceMutation{
		{"maintenance omitted", func(t *testing.T, job map[string]any) {
			t.Helper()

			delete(goMaintenanceMap(t, job, "with"), "maintenance-default-branch")
		}, "boolean opt-in"},
		{"maintenance disabled", func(t *testing.T, job map[string]any) {
			t.Helper()

			goMaintenanceMap(t, job, "with")["maintenance-default-branch"] = false
		}, "boolean opt-in"},
		{"maintenance string", func(t *testing.T, job map[string]any) {
			t.Helper()

			goMaintenanceMap(t, job, "with")["maintenance-default-branch"] = "true"
		}, "boolean opt-in"},
		{"signed fixes omitted", func(t *testing.T, job map[string]any) {
			t.Helper()

			delete(goMaintenanceMap(t, job, "with"), "apply-signed-fixes")
		}, "signed-fix opt-out"},
		{"signed fixes enabled", func(t *testing.T, job map[string]any) {
			t.Helper()

			goMaintenanceMap(t, job, "with")["apply-signed-fixes"] = true
		}, "signed-fix opt-out"},
		{"signed fixes string", func(t *testing.T, job map[string]any) {
			t.Helper()

			goMaintenanceMap(t, job, "with")["apply-signed-fixes"] = "false"
		}, "signed-fix opt-out"},
	}
}

func goMaintenanceEnvelopeMutations() []goMaintenanceMutation {
	return []goMaintenanceMutation{
		{"new OIDC grant", func(t *testing.T, job map[string]any) {
			t.Helper()

			goMaintenanceMap(t, job, "permissions")["id-token"] = "write"
		}, "credential envelope"},
		{"new secret", func(t *testing.T, job map[string]any) {
			t.Helper()

			goMaintenanceMap(t, job, "secrets")["EXTRA"] = "${{ secrets.EXTRA }}"
		}, "credential envelope"},
		{"inherited secrets", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["secrets"] = "inherit"
		}, "explicit mappings"},
		{"different implementation", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["uses"] = strings.Replace(
				goMaintenanceString(t, job, "uses"),
				"validate-go-project",
				"validate-node-project",
				1,
			)
		}, "reviewed bare reusable call"},
		{"local execution added", func(t *testing.T, job map[string]any) {
			t.Helper()

			job["steps"] = []any{map[string]any{"run": "true"}}
		}, "reviewed bare reusable call"},
	}
}

func TestGoMaintenanceCallerRejectsBoundaryWidening(t *testing.T) {
	t.Parallel()

	contents := readRepoFile(t, ".github/workflows/ci.yaml")

	cases := append(goMaintenanceAdmissionMutations(), goMaintenanceInputMutations()...)
	cases = append(cases, goMaintenanceEnvelopeMutations()...)

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var document map[string]any
			require.NoError(t, yaml.Unmarshal(contents, &document))
			job := goMaintenanceMap(t, goMaintenanceMap(t, document, "jobs"), "ci-go")
			before, err := yaml.Marshal(job)
			require.NoError(t, err)
			test.mutate(t, job)
			after, err := yaml.Marshal(job)
			require.NoError(t, err)
			require.NotEqual(
				t,
				string(before),
				string(after),
				"mutation must actually change the caller",
			)

			mutant, err := yaml.Marshal(document)
			require.NoError(t, err)
			require.ErrorContains(t, validateGoMaintenanceCaller(mutant), test.want)
		})
	}
}

func TestGoMaintenanceCallerAcceptsFormatting(t *testing.T) {
	t.Parallel()

	var document map[string]any
	require.NoError(t, yaml.Unmarshal(readRepoFile(t, ".github/workflows/ci.yaml"), &document))
	job := goMaintenanceMap(t, goMaintenanceMap(t, document, "jobs"), "ci-go")
	job["if"] = "\n\t" + strings.ReplaceAll(goMaintenanceString(t, job, "if"), "&&", "\n && \t")
	formatted, err := yaml.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, validateGoMaintenanceCaller(formatted))
}

func goMaintenanceMap(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := values[key].(map[string]any)
	require.True(t, ok, "%s must be a mapping", key)

	return value
}

func goMaintenanceString(t *testing.T, values map[string]any, key string) string {
	t.Helper()

	value, ok := values[key].(string)
	require.True(t, ok, "%s must be a string", key)

	return value
}

package celrules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/celrules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRulesFile writes a temp rules file and returns its path.
func writeRulesFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "validation-rules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// deployment returns a minimal decoded Deployment-shaped document.
func deployment(image string) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "app"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "app", "image": image},
					},
				},
			},
		},
	}
}

func TestLoadRules_ValidFileDefaultsSeverity(t *testing.T) {
	t.Parallel()

	path := writeRulesFile(t, `
rules:
  - name: no-latest-tag
    expression: "object.kind != 'Deployment'"
    message: images must not use :latest
  - name: warn-rule
    expression: "true"
    severity: warning
`)

	rules, err := celrules.LoadRules(path)
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, celrules.SeverityError, rules[0].Severity)
	assert.Equal(t, celrules.SeverityWarning, rules[1].Severity)
}

func TestLoadRules_Errors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		content string
		wantErr string
	}{
		{"empty rules", "rules: []", "declares no rules"},
		{"missing name", "rules:\n  - expression: 'true'", "has no name"},
		{
			"duplicate name",
			"rules:\n  - name: a\n    expression: 'true'\n  - name: a\n    expression: 'false'",
			"duplicate rule name",
		},
		{"missing expression", "rules:\n  - name: a", "has no expression"},
		{
			"bad severity",
			"rules:\n  - name: a\n    expression: 'true'\n    severity: fatal",
			"invalid severity",
		},
		{
			"unknown field",
			"rules:\n  - name: a\n    expression: 'true'\n    expr: dup",
			"parse rules file",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := celrules.LoadRules(writeRulesFile(t, testCase.content))
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestLoadRules_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := celrules.LoadRules(filepath.Join(t.TempDir(), "absent.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read rules file")
}

func TestNewEngine_CompileErrorNamesRule(t *testing.T) {
	t.Parallel()

	_, err := celrules.NewEngine([]celrules.Rule{
		{Name: "broken", Expression: "object.kind ==", Severity: celrules.SeverityError},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compile rule broken")
}

func TestNewEngine_NonBoolExpressionNamesRule(t *testing.T) {
	t.Parallel()

	_, err := celrules.NewEngine([]celrules.Rule{
		{Name: "stringy", Expression: "object.kind", Severity: celrules.SeverityError},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must evaluate to a boolean")
	assert.Contains(t, err.Error(), "stringy")
}

func TestEvaluate_PassProducesNoViolation(t *testing.T) {
	t.Parallel()

	engine, err := celrules.NewEngine([]celrules.Rule{
		{
			Name:       "kind-known",
			Expression: "object.kind == 'Deployment'",
			Severity:   celrules.SeverityError,
		},
	})
	require.NoError(t, err)

	violations := engine.Evaluate(deployment("app:v1.2.3"))
	assert.Empty(t, violations)
}

func TestEvaluate_FailProducesAttributedViolation(t *testing.T) {
	t.Parallel()

	engine, err := celrules.NewEngine([]celrules.Rule{
		{
			Name:       "no-latest-tag",
			Expression: "object.spec.template.spec.containers.all(c, !c.image.endsWith(':latest'))",
			Message:    "images must be pinned, not :latest",
			Severity:   celrules.SeverityError,
		},
	})
	require.NoError(t, err)

	violations := engine.Evaluate(deployment("app:latest"))
	require.Len(t, violations, 1)
	assert.Equal(t, "no-latest-tag", violations[0].Rule)
	assert.Equal(t, "images must be pinned, not :latest", violations[0].Message)
	assert.Equal(t, celrules.SeverityError, violations[0].Severity)
}

func TestEvaluate_DefaultMessageIncludesExpression(t *testing.T) {
	t.Parallel()

	engine, err := celrules.NewEngine([]celrules.Rule{
		{Name: "always-false", Expression: "false", Severity: celrules.SeverityWarning},
	})
	require.NoError(t, err)

	violations := engine.Evaluate(deployment("app:v1"))
	require.Len(t, violations, 1)
	assert.Contains(t, violations[0].Message, "false")
	assert.Equal(t, celrules.SeverityWarning, violations[0].Severity)
}

func TestEvaluate_EvalErrorFailsClosed(t *testing.T) {
	t.Parallel()

	engine, err := celrules.NewEngine([]celrules.Rule{
		{
			Name:       "needs-annotations",
			Expression: "object.metadata.annotations.size() > 0",
			Severity:   celrules.SeverityError,
		},
	})
	require.NoError(t, err)

	// The document has no metadata.annotations key, so evaluation errors —
	// which must surface as a violation (fail-closed), not a silent pass.
	violations := engine.Evaluate(deployment("app:v1"))
	require.Len(t, violations, 1)
	assert.Equal(t, "needs-annotations", violations[0].Rule)
	assert.Contains(t, violations[0].Message, "rule evaluation failed")
}

func TestEvaluate_MultipleRulesReportIndependently(t *testing.T) {
	t.Parallel()

	engine, err := celrules.NewEngine([]celrules.Rule{
		{Name: "pass", Expression: "object.kind == 'Deployment'", Severity: celrules.SeverityError},
		{Name: "fail", Expression: "object.kind == 'Service'", Severity: celrules.SeverityWarning},
	})
	require.NoError(t, err)

	violations := engine.Evaluate(deployment("app:v1"))
	require.Len(t, violations, 1)
	assert.Equal(t, "fail", violations[0].Rule)
}

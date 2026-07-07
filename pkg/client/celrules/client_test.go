package celrules_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/celrules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const deploymentWithReplicas = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 3
`

const deploymentNoReplicas = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
`

const serviceManifest = `apiVersion: v1
kind: Service
metadata:
  name: web
`

func rule(name, expression, message string, severity celrules.Severity) celrules.Rule {
	return celrules.Rule{Name: name, Expression: expression, Message: message, Severity: severity}
}

func TestValidate_RulePasses(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	rules := []celrules.Rule{
		rule(
			"is-deployment",
			"object.kind == 'Deployment'",
			"must be a Deployment",
			celrules.SeverityError,
		),
	}

	report, err := client.Validate(
		t.Context(),
		rules,
		[][]byte{[]byte(deploymentWithReplicas)},
		nil,
	)

	require.NoError(t, err)
	assert.Empty(t, report.Violations)
	assert.False(t, report.HasErrors())
}

func TestValidate_RuleFailsIsAttributed(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	rules := []celrules.Rule{
		rule(
			"replicas-required",
			"object.kind != 'Deployment' || (has(object.spec) && has(object.spec.replicas))",
			"Deployments must set spec.replicas",
			celrules.SeverityError,
		),
	}

	report, err := client.Validate(t.Context(), rules, [][]byte{[]byte(deploymentNoReplicas)}, nil)

	require.NoError(t, err)
	require.Len(t, report.Violations, 1)
	violation := report.Violations[0]
	assert.Equal(t, "replicas-required", violation.Rule)
	assert.Equal(t, "Deployment/web", violation.Resource)
	assert.Equal(t, "Deployments must set spec.replicas", violation.Message)
	assert.Equal(t, celrules.SeverityError, violation.Severity)
	assert.True(t, report.HasErrors())
}

func TestValidate_AttributionAppendsSource(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	rules := []celrules.Rule{
		rule("never", "false", "always fails", celrules.SeverityError),
	}
	opts := &celrules.Options{
		Attribution: map[string]string{"Deployment/default/web": "HelmRelease flux-system/web"},
	}

	report, err := client.Validate(
		t.Context(),
		rules,
		[][]byte{[]byte(deploymentWithReplicas)},
		opts,
	)

	require.NoError(t, err)
	require.Len(t, report.Violations, 1)
	assert.Equal(
		t,
		"Deployment/default/web (HelmRelease flux-system/web)",
		report.Violations[0].Resource,
	)
}

func TestValidate_SeverityHandling(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	rules := []celrules.Rule{
		rule("warn", "false", "advisory", celrules.SeverityWarning),
		rule("info", "false", "note", celrules.SeverityInfo),
	}

	report, err := client.Validate(t.Context(), rules, [][]byte{[]byte(serviceManifest)}, nil)

	require.NoError(t, err)
	assert.Len(t, report.Violations, 2)
	assert.False(t, report.HasErrors(), "warning/info violations must not fail validation")
	assert.Equal(t, 1, report.Count(celrules.SeverityWarning))
	assert.Equal(t, 1, report.Count(celrules.SeverityInfo))
	assert.Equal(t, 0, report.Count(celrules.SeverityError))
}

func TestValidate_InvalidExpressionNamesRule(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()

	tests := []struct {
		name       string
		expression string
	}{
		{name: "syntax error", expression: "object.spec.replicas >"},
		{name: "unknown function", expression: "nonexistentFunc(object)"},
		{name: "non-bool result", expression: "size(object)"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rules := []celrules.Rule{
				rule("broken-rule", testCase.expression, "msg", celrules.SeverityError),
			}

			report, err := client.Validate(
				t.Context(),
				rules,
				[][]byte{[]byte(serviceManifest)},
				nil,
			)

			require.Error(t, err)
			require.ErrorIs(t, err, celrules.ErrRuleCompilation)
			assert.Contains(
				t,
				err.Error(),
				"broken-rule",
				"compile error must name the offending rule",
			)
			assert.Empty(t, report.Violations)
		})
	}
}

func TestValidate_RuntimeEvalErrorBecomesViolation(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	// A Service has no spec.replicas; unguarded access errors at runtime.
	rules := []celrules.Rule{
		rule("bad-guard", "object.spec.replicas > 0", "needs replicas", celrules.SeverityError),
	}

	report, err := client.Validate(t.Context(), rules, [][]byte{[]byte(serviceManifest)}, nil)

	require.NoError(t, err)
	require.Len(t, report.Violations, 1)
	assert.Equal(t, "bad-guard", report.Violations[0].Rule)
	assert.Contains(t, report.Violations[0].Message, "evaluation error")
}

func TestValidate_EmptyInputs(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	rules := []celrules.Rule{rule("r", "true", "m", celrules.SeverityError)}

	tests := []struct {
		name  string
		rules []celrules.Rule
		docs  [][]byte
	}{
		{name: "no rules", rules: nil, docs: [][]byte{[]byte(serviceManifest)}},
		{name: "no documents", rules: rules, docs: nil},
		{name: "blank document skipped", rules: rules, docs: [][]byte{[]byte("\n---\n")}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			report, err := client.Validate(t.Context(), testCase.rules, testCase.docs, nil)

			require.NoError(t, err)
			assert.Empty(t, report.Violations)
		})
	}
}

func TestValidate_MultipleDocumentsAndRules(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	rules := []celrules.Rule{
		rule(
			"no-service",
			"object.kind != 'Service'",
			"no Services allowed",
			celrules.SeverityError,
		),
		rule(
			"named-web",
			"object.metadata.name == 'web'",
			"must be named web",
			celrules.SeverityWarning,
		),
	}
	docs := [][]byte{[]byte(deploymentWithReplicas), []byte(serviceManifest)}

	report, err := client.Validate(t.Context(), rules, docs, nil)

	require.NoError(t, err)
	// Only the Service violates no-service; both are named web so named-web never fails.
	require.Len(t, report.Violations, 1)
	assert.Equal(t, "no-service", report.Violations[0].Rule)
	assert.Equal(t, "Service/web", report.Violations[0].Resource)
}

func TestValidate_ContextCancelled(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	rules := []celrules.Rule{rule("r", "true", "m", celrules.SeverityError)}

	report, err := client.Validate(ctx, rules, [][]byte{[]byte(serviceManifest)}, nil)

	// A cancelled context aborts validation with the cancellation cause rather
	// than masking it as a spurious evaluation-error violation.
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, report.Violations)
}

func TestValidate_MalformedDocumentSurfacesError(t *testing.T) {
	t.Parallel()

	client := celrules.NewClient()
	rules := []celrules.Rule{rule("r", "true", "m", celrules.SeverityError)}

	// A bare scalar cannot unmarshal into an object; a broken manifest must
	// surface as an error, not slip through the empty-document skip and return
	// a false success.
	report, err := client.Validate(
		t.Context(),
		rules,
		[][]byte{[]byte("scalar-not-a-mapping")},
		nil,
	)

	require.Error(t, err)
	assert.Empty(t, report.Violations)
}

package kyvernopolicy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configMapRule is a Deny ValidatingPolicy for ConfigMaps with one validation.
// The arguments are the policy name and the validation's YAML lines, already
// indented under `- `.
const configMapRule = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: %s
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  validations:
  - %s
`

func configMapWithData(namespace string, data map[string]any) map[string]any {
	document := configMap(namespace, nil)
	document["data"] = data

	return document
}

// evaluatedCase is a ConfigMap validation that reads data[key] and rejects the
// value blocked ("blocked" when empty).
type evaluatedCase struct {
	validation string
	key        string
	blocked    string
}

// httpWordCases mention the word http only as a field, map key, string
// literal, local variable or static message.
func httpWordCases() map[string]evaluatedCase {
	return map[string]evaluatedCase{
		"comprehension variable named http": {
			validation: `expression: "[object.?data.?mode.orValue('')].all(http, http != 'blocked')"`,
			key:        "mode",
		},
		"string literal naming a library call": {
			validation: `expression: "object.?data.?mode.orValue('') != 'http.Get'"`,
			key:        "mode",
			blocked:    "http.Get",
		},
		"optional field selection": {
			validation: `expression: "object.?data.?http.orValue('') != 'blocked'"`,
			key:        "http",
		},
		"field selection": {
			validation: `expression: "!has(object.data) || !has(object.data.http) || ` +
				`object.data.http != 'blocked'"`,
			key: "http",
		},
		"optional index": {
			validation: `expression: "object.?data[?'http'].orValue('') != 'blocked'"`,
			key:        "http",
		},
		"string literal index": {
			validation: `expression: "!('http' in object.?data.orValue({})) || ` +
				`object.data['http'] != 'blocked'"`,
			key: "http",
		},
		"static message naming a URL": {
			validation: `expression: "object.?data.?mode.orValue('') != 'blocked'"` +
				"\n    message: \"see http://docs.example/policy before setting mode\"",
			key: "mode",
		},
	}
}

// A field, map key, string literal, local variable or message that merely
// contains the word http is not the CEL http library. The policy reading it must
// be evaluated offline like any other: a compliant document passes and a
// violating one is blocked, not reported as unsupported. An Ingress reads its
// paths through exactly such a field.
func TestCELEvaluate_HTTPWordOutsideTheLibraryIsEvaluated(t *testing.T) {
	t.Parallel()

	for name, testCase := range httpWordCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			engine := celEngine(t, nil, validatingPolicy(
				t, sprintf(configMapRule, "reads-an-http-field", testCase.validation),
			))

			allowed, err := engine.Evaluate(
				t.Context(),
				configMapWithData("default", map[string]any{testCase.key: "allowed"}),
			)
			require.NoError(t, err)
			assert.Empty(t, allowed)

			blockedValue := testCase.blocked
			if blockedValue == "" {
				blockedValue = "blocked"
			}

			blocked, err := engine.Evaluate(
				t.Context(),
				configMapWithData("default", map[string]any{testCase.key: blockedValue}),
			)
			require.NoError(t, err)
			require.Len(t, blocked, 1)
			assert.False(t, blocked[0].Unsupported, blocked[0].Message)
			assert.False(t, blocked[0].Error, blocked[0].Message)
			assert.True(t, blocked[0].Blocking)
		})
	}
}

// Wherever an expression calls the http library, the policy is reported as not
// evaluable offline and never compiled or run, so validation cannot reach the
// network. That holds inside optional syntax, in a message expression, and for
// an expression the identifier check cannot parse at all.
func TestCELEvaluate_HTTPLibraryAnywhereIsNeverEvaluated(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"inside optional syntax": `expression: "object.?data.?verdict.orValue(` +
			`http.Get('http://127.0.0.1:1/verdict').verdict) == 'allowed'"`,
		"conditional call": `expression: "!has(object.data) ? true : ` +
			`http.Get('http://127.0.0.1:1/verdict').allowed"`,
		"message expression": `expression: "false"` +
			"\n    messageExpression: \"http.Get('http://127.0.0.1:1/reason').reason\"",
		"expression the check cannot parse": `expression: "http.Get('http://127.0.0.1:1/verdict') =="`,
		// A leading dot names the identifier in the root scope; it is the same
		// variable.
		"absolute identifier": `expression: ".http.Get('http://127.0.0.1:1/verdict').allowed == true"`,
		"absolute identifier the check cannot parse": `expression: ` +
			`".http.Get('https://127.0.0.1:1/verdict') =="`,
		// A comprehension's range is evaluated outside the scope it opens.
		"library in a comprehension range": `expression: ` +
			`"[http.Get('http://127.0.0.1:1/verdict')].all(v, v.allowed == true)"`,
		"library in a comprehension body": `expression: ` +
			`"['a'].all(v, http.Get('http://127.0.0.1:1/verdict').allowed == true)"`,
		// A local http shadows only the plain name, never the root-scoped one.
		"root-scoped library under a local http": `expression: ` +
			`"['a'].all(http, .http.Get('http://127.0.0.1:1/verdict').allowed == true)"`,
	}

	for name, validation := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			engine := celEngine(t, nil, validatingPolicy(
				t, sprintf(configMapRule, "asks-a-service", validation),
			))

			violations, err := engine.Evaluate(
				t.Context(),
				configMapWithData("default", map[string]any{"verdict": "allowed"}),
			)
			require.NoError(t, err)
			require.Len(t, violations, 1)
			assert.True(t, violations[0].Unsupported)
			assert.False(t, violations[0].Blocking)
			assert.Contains(t, violations[0].Message, "http library")
		})
	}
}

// Only the namespaceObject variable depends on the namespace. A ConfigMap key of
// that name is ordinary data, so a policy reading it is evaluated even when the
// document's namespace is not among the rendered documents.
func TestCELEvaluate_NamespaceObjectFieldIsNotTheNamespace(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, sprintf(
		configMapRule,
		"reads-a-namespace-object-key",
		`expression: "object.?data.?namespaceObject.orValue('') != 'blocked'"`,
	)))

	blocked, err := engine.Evaluate(
		t.Context(),
		configMapWithData("prod", map[string]any{"namespaceObject": "blocked"}),
	)
	require.NoError(t, err)
	require.Len(t, blocked, 1)
	assert.False(t, blocked[0].Unsupported, blocked[0].Message)
	assert.False(t, blocked[0].Error, blocked[0].Message)
	assert.True(t, blocked[0].Blocking)
}

// A leading dot names namespaceObject in the root scope, so the policy still
// reads the namespace: with that namespace missing from the rendered documents,
// it is reported as not evaluable offline instead of guessed.
func TestCELEvaluate_AbsoluteNamespaceObjectReadsTheNamespace(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, sprintf(
		configMapRule,
		"team-matches-namespace",
		`expression: ".namespaceObject.metadata.labels.team == object.metadata.labels.team"`,
	)))

	violations, err := engine.Evaluate(
		t.Context(),
		configMap("prod", map[string]any{"team": "platform"}),
	)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Unsupported, violations[0].Message)
	assert.False(t, violations[0].Blocking)
	assert.Contains(
		t,
		violations[0].Message,
		`namespace "prod" is not among the rendered documents`,
	)
}

// With the namespace rendered, a policy reading the root-scoped namespaceObject
// is evaluated against it: a matching team passes, a mismatched one is blocked.
func TestCELEvaluate_AbsoluteNamespaceObjectWithRenderedNamespaceIsEvaluated(t *testing.T) {
	t.Parallel()

	engine := celEngine(
		t,
		[]map[string]any{namespace("prod", map[string]any{"team": "platform"})},
		validatingPolicy(t, sprintf(
			configMapRule,
			"team-matches-namespace",
			`expression: ".namespaceObject.metadata.labels.team == object.metadata.labels.team"`,
		)),
	)

	matching, err := engine.Evaluate(
		t.Context(),
		configMap("prod", map[string]any{"team": "platform"}),
	)
	require.NoError(t, err)
	assert.Empty(t, matching)

	mismatched, err := engine.Evaluate(
		t.Context(),
		configMap("prod", map[string]any{"team": "other"}),
	)
	require.NoError(t, err)
	require.Len(t, mismatched, 1)
	assert.False(t, mismatched[0].Unsupported, mismatched[0].Message)
	assert.False(t, mismatched[0].Error, mismatched[0].Message)
	assert.True(t, mismatched[0].Blocking)
}

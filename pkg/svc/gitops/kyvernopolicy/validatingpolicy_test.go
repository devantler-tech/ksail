package kyvernopolicy_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	policiesv1beta1 "github.com/kyverno/api/api/policies.kyverno.io/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const requireTeamValidatingPolicy = `
apiVersion: policies.kyverno.io/%s
kind: ValidatingPolicy
metadata:
  name: require-team-label
spec:
  validationActions: [%s]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE", "UPDATE"]
      resources: ["configmaps"]
  validations:
  - expression: "has(object.metadata.labels) && 'team' in object.metadata.labels"
    message: "label team is required"
`

func validatingPolicy(t *testing.T, content string) policiesv1beta1.ValidatingPolicyLike {
	t.Helper()

	doc := decodeYAML(t, content)
	require.True(t, kyvernopolicy.IsValidatingPolicy(doc))

	decoded, err := kyvernopolicy.DecodeValidatingPolicy(doc)
	require.NoError(t, err)

	return decoded
}

func celEngine(
	t *testing.T,
	namespaces []map[string]any,
	policies ...policiesv1beta1.ValidatingPolicyLike,
) *kyvernopolicy.CELEngine {
	t.Helper()

	engine, err := kyvernopolicy.NewCELEngine(policies, namespaces)
	require.NoError(t, err)

	return engine
}

func requireTeamCEL(t *testing.T, version, action string) policiesv1beta1.ValidatingPolicyLike {
	t.Helper()

	return validatingPolicy(t, sprintf(requireTeamValidatingPolicy, version, action))
}

func TestIsValidatingPolicy(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		apiVersion, kind string
		want             bool
	}{
		{"policies.kyverno.io/v1", "ValidatingPolicy", true},
		{"policies.kyverno.io/v1beta1", "ValidatingPolicy", true},
		{"policies.kyverno.io/v1alpha1", "ValidatingPolicy", true},
		{"policies.kyverno.io/v1", "NamespacedValidatingPolicy", true},
		{"policies.kyverno.io/v1", "MutatingPolicy", false},
		{"policies.kyverno.io/v2", "ValidatingPolicy", false},
		{"kyverno.io/v1", "ValidatingPolicy", false},
		{"policies.kyverno.io", "ValidatingPolicy", false},
	} {
		doc := map[string]any{"apiVersion": testCase.apiVersion, "kind": testCase.kind}
		assert.Equal(
			t,
			testCase.want,
			kyvernopolicy.IsValidatingPolicy(doc),
			"%s %s",
			testCase.apiVersion,
			testCase.kind,
		)
	}
}

func TestCELEvaluate_DenyFailureIsBlocking(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"v1", "v1beta1", "v1alpha1"} {
		engine := celEngine(t, nil, requireTeamCEL(t, version, "Deny"))

		violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
		require.NoError(t, err)
		require.Len(t, violations, 1, version)
		assert.Equal(t, "require-team-label", violations[0].Policy)
		assert.Contains(t, violations[0].Message, "label team is required")
		assert.True(t, violations[0].Blocking, version)
		assert.False(t, violations[0].Error)
		assert.False(t, violations[0].Unsupported)
	}
}

func TestCELEvaluate_CompliantDocumentPasses(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, requireTeamCEL(t, "v1", "Deny"))

	violations, err := engine.Evaluate(
		t.Context(),
		configMap("default", map[string]any{"team": "platform"}),
	)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestCELEvaluate_UnmatchedKindIsIgnored(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, requireTeamCEL(t, "v1", "Deny"))

	secret := configMap("default", nil)
	secret["kind"] = "Secret"

	violations, err := engine.Evaluate(t.Context(), secret)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestCELEvaluate_AuditAndWarnFailuresDoNotBlock(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"Audit", "Warn"} {
		engine := celEngine(t, nil, requireTeamCEL(t, "v1", action))

		violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
		require.NoError(t, err)
		require.Len(t, violations, 1, action)
		assert.False(t, violations[0].Blocking, action)
		assert.False(t, violations[0].Unsupported, action)
	}
}

func TestCELEvaluate_AdmissionDisabledPolicyIsSkipped(t *testing.T) {
	t.Parallel()

	policy := requireTeamCEL(t, "v1", "Deny")
	disabled := false
	spec := policy.GetValidatingPolicySpec()
	spec.EvaluationConfiguration = &policiesv1beta1.EvaluationConfiguration{
		Admission: &policiesv1beta1.AdmissionConfiguration{Enabled: &disabled},
	}

	engine := celEngine(t, nil, policy)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	assert.Empty(t, violations)
}

const namespacedRequireTeam = `
apiVersion: policies.kyverno.io/v1
kind: NamespacedValidatingPolicy
metadata:
  name: require-team-label
  namespace: apps
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  validations:
  - expression: "has(object.metadata.labels) && 'team' in object.metadata.labels"
    message: "label team is required"
`

func TestCELEvaluate_NamespacedPolicyAppliesOnlyToItsNamespace(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, namespacedRequireTeam))

	inside, err := engine.Evaluate(t.Context(), configMap("apps", nil))
	require.NoError(t, err)
	require.Len(t, inside, 1)
	assert.Equal(t, "apps/require-team-label", inside[0].Policy)
	assert.True(t, inside[0].Blocking)

	outside, err := engine.Evaluate(t.Context(), configMap("other", nil))
	require.NoError(t, err)
	assert.Empty(t, outside)
}

const selectsProductionNamespaces = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: require-team-in-production
spec:
  validationActions: [Deny]
  matchConstraints:
    namespaceSelector:
      matchLabels:
        tier: production
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  validations:
  - expression: "has(object.metadata.labels) && 'team' in object.metadata.labels"
    message: "label team is required"
`

func TestCELEvaluate_NamespaceSelectorUsesRenderedNamespaceLabels(t *testing.T) {
	t.Parallel()

	engine := celEngine(
		t,
		[]map[string]any{
			namespace("prod", map[string]any{"tier": "production"}),
			namespace("dev", map[string]any{"tier": "development"}),
		},
		validatingPolicy(t, selectsProductionNamespaces),
	)

	prod, err := engine.Evaluate(t.Context(), configMap("prod", nil))
	require.NoError(t, err)
	require.Len(t, prod, 1)
	assert.True(t, prod[0].Blocking)

	dev, err := engine.Evaluate(t.Context(), configMap("dev", nil))
	require.NoError(t, err)
	assert.Empty(t, dev)
}

func TestCELEvaluate_UnknownNamespaceWithSelectorIsUnsupported(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, selectsProductionNamespaces))

	violations, err := engine.Evaluate(t.Context(), configMap("prod", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Unsupported)
	assert.False(t, violations[0].Blocking)
	assert.Contains(
		t,
		violations[0].Message,
		`namespace "prod" is not among the rendered documents`,
	)
}

const resourceLookupPolicy = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: require-existing-owner
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  variables:
  - name: owner
    expression: "resource.Get('v1', 'serviceaccounts', object.metadata.namespace, 'owner')"
  validations:
  - expression: "variables.owner.metadata.name == 'owner'"
    message: "the owner service account must exist"
`

func TestCELEvaluate_ClusterLookupIsUnsupportedNotBlocking(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, resourceLookupPolicy))

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Unsupported, violations[0].Message)
	assert.False(t, violations[0].Blocking)
}

const httpPolicy = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: ask-a-service
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  variables:
  - name: verdict
    expression: "http.Get('http://127.0.0.1:1/verdict')"
  validations:
  - expression: "variables.verdict.allowed == true"
    message: "the service denied it"
`

func TestCELEvaluate_HTTPPolicyIsNeverEvaluated(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, httpPolicy))

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Unsupported)
	assert.False(t, violations[0].Blocking)
	assert.Contains(t, violations[0].Message, "http library")
}

const brokenExpressionPolicy = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: broken
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  validations:
  - expression: "object.metadata.labels.team =="
`

func TestNewCELEngine_UncompilablePolicyIsAnError(t *testing.T) {
	t.Parallel()

	_, err := kyvernopolicy.NewCELEngine(
		[]policiesv1beta1.ValidatingPolicyLike{validatingPolicy(t, brokenExpressionPolicy)},
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken")
}

func secret(namespace string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "credentials", "namespace": namespace},
	}
}

// A policy that cannot be evaluated offline is only worth reporting for the
// documents it would select: a Secret matches neither ConfigMap policy, so it
// must produce no warning even though both are unsupported offline.
func TestCELEvaluate_UnsupportedPolicyIgnoresDocumentsItDoesNotMatch(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"http library":              httpPolicy,
		"unknown namespace labels":  selectsProductionNamespaces,
		"namespaceObject reference": readsNamespaceObject,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			engine := celEngine(t, nil, validatingPolicy(t, content))

			violations, err := engine.Evaluate(t.Context(), secret("prod"))
			require.NoError(t, err)
			assert.Empty(t, violations)
		})
	}
}

const readsNamespaceObject = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: team-matches-namespace
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  validations:
  - expression: "namespaceObject.metadata.labels.team == object.metadata.labels.team"
    message: "the team label must match the namespace's"
`

// An expression that reads namespaceObject for a namespace missing from the
// rendered documents would be evaluated against nothing, so it is reported as
// not evaluable offline instead of guessed.
func TestCELEvaluate_UnknownNamespaceReadByExpressionIsUnsupported(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, readsNamespaceObject))

	violations, err := engine.Evaluate(
		t.Context(),
		configMap("prod", map[string]any{"team": "platform"}),
	)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Unsupported)
	assert.False(t, violations[0].Blocking)
	assert.Contains(
		t,
		violations[0].Message,
		`namespace "prod" is not among the rendered documents`,
	)
}

// A namespaced document that declares no namespace lands wherever its applier
// puts it, so a policy that depends on that namespace cannot be evaluated
// offline. Evaluating it against the empty namespace would treat the document as
// cluster-scoped and guess a verdict.
func TestCELEvaluate_UndeclaredNamespaceIsUnsupported(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		policy string
		labels map[string]any
	}{
		"namespaceSelector": {policy: selectsProductionNamespaces},
		"namespaceObject": {
			policy: readsNamespaceObject,
			labels: map[string]any{"team": "platform"},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			engine := celEngine(t, nil, validatingPolicy(t, testCase.policy))

			violations, err := engine.Evaluate(t.Context(), configMap("", testCase.labels))
			require.NoError(t, err)
			require.Len(t, violations, 1)
			assert.True(t, violations[0].Unsupported)
			assert.False(t, violations[0].Blocking)
			assert.Contains(t, violations[0].Message, "the document declares no namespace")
		})
	}
}

const selectsProductionClusterRoles = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: require-team-on-cluster-roles
spec:
  validationActions: [Deny]
  matchConstraints:
    namespaceSelector:
      matchLabels:
        tier: production
    resourceRules:
    - apiGroups: ["rbac.authorization.k8s.io"]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["clusterroles"]
  validations:
  - expression: "has(object.metadata.labels) && 'team' in object.metadata.labels"
    message: "label team is required"
`

// A cluster-scoped kind has no namespace to know, and a cluster matches it
// against a namespaceSelector just as the offline engine does, so it is
// evaluated normally rather than reported as unknown.
func TestCELEvaluate_ClusterScopedKindWithoutNamespaceIsEvaluated(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, selectsProductionClusterRoles))

	violations, err := engine.Evaluate(t.Context(), map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1",
		"kind":       "ClusterRole",
		"metadata":   map[string]any{"name": "reader"},
	})
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.False(t, violations[0].Unsupported)
	assert.True(t, violations[0].Blocking)
}

// With the namespace rendered, the same policy is evaluated normally.
func TestCELEvaluate_RenderedNamespaceReadByExpressionIsEvaluated(t *testing.T) {
	t.Parallel()

	engine := celEngine(
		t,
		[]map[string]any{namespace("prod", map[string]any{"team": "platform"})},
		validatingPolicy(t, readsNamespaceObject),
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
	assert.True(t, mismatched[0].Blocking)
}

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
		"static message shaped like a library call": {
			validation: `expression: "object.?data.?mode.orValue('') != 'blocked'"` +
				"\n    message: \"http.Get('https://docs.example/policy')\"",
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
		"library in a map value": `expression: ` +
			`"{'verdict': http.Get('http://127.0.0.1:1/verdict')}.verdict.allowed == true"`,
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

// A local comprehension variable or a static message that merely names
// namespaceObject does not read the Namespace, so the policy is evaluated even
// when the document's namespace is not among the rendered documents.
func TestCELEvaluate_NamespaceObjectWordOutsideTheVariableIsEvaluated(t *testing.T) {
	t.Parallel()

	for name, validation := range map[string]string{
		"comprehension variable named namespaceObject": `expression: ` +
			`"[object.?data.?mode.orValue('')].all(namespaceObject, namespaceObject != 'blocked')"`,
		"static message naming namespaceObject": `expression: ` +
			`"object.?data.?mode.orValue('') != 'blocked'"` +
			"\n    message: \"check namespaceObject\"",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			engine := celEngine(t, nil, validatingPolicy(
				t, sprintf(configMapRule, "names-namespace-object", validation),
			))

			blocked, err := engine.Evaluate(
				t.Context(),
				configMapWithData("prod", map[string]any{"mode": "blocked"}),
			)
			require.NoError(t, err)
			require.Len(t, blocked, 1)
			assert.False(t, blocked[0].Unsupported, blocked[0].Message)
			assert.False(t, blocked[0].Error, blocked[0].Message)
			assert.True(t, blocked[0].Blocking)
		})
	}
}

// A message expression is evaluated, so one that reads namespaceObject makes the
// policy depend on the namespace: with that namespace not rendered, the policy
// is reported as not evaluable offline instead of guessed.
func TestCELEvaluate_NamespaceObjectInMessageExpressionIsUnsupported(t *testing.T) {
	t.Parallel()

	engine := celEngine(t, nil, validatingPolicy(t, sprintf(
		configMapRule,
		"namespace-in-message",
		`expression: "false"`+"\n    messageExpression: \"'denied in ' + namespaceObject.metadata.name\"",
	)))

	violations, err := engine.Evaluate(t.Context(), configMap("prod", nil))
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

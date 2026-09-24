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

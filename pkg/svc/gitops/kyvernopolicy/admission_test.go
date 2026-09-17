package kyvernopolicy_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// erroringPolicy has one validate rule whose context entry cannot load offline, so the engine
// reports the rule as errored rather than failed. %[1]s is the failure action and %[2]s the
// optional spec lines (such as failurePolicy).
const erroringPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: erroring
spec:
  validationFailureAction: %[1]s
%[2]s
  rules:
  - name: needs-cluster
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    context:
    - name: kube
      apiCall:
        urlPath: /api/v1/namespaces/kube-system/secrets
    validate:
      message: "kubernetes context must stay unloaded"
      deny:
        conditions:
          any:
          - key: "{{ kube }}"
            operator: Equals
            value: "unreachable"
`

func namespace(name string, labels map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": name, "labels": labels},
	}
}

func TestEvaluate_PolicyWithAdmissionDisabledIsIgnored(t *testing.T) {
	t.Parallel()

	backgroundOnly := policy(t, `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team-label
spec:
  validationFailureAction: Enforce
  admission: false
  background: true
  rules:
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`)
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{backgroundOnly}, nil)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	assert.Empty(t, violations, "Kyverno never evaluates an admission: false policy at admission")
}

func TestEvaluate_ErrorBlockingFollowsFailurePolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		action   string
		spec     string
		blocking bool
	}{
		{name: "enforce with default Fail blocks", action: "Enforce", blocking: true},
		{
			name:     "enforce with failurePolicy Ignore does not block",
			action:   "Enforce",
			spec:     "  failurePolicy: Ignore",
			blocking: false,
		},
		{
			name:     "audit with failurePolicy Fail does not block",
			action:   "Audit",
			spec:     "  failurePolicy: Fail",
			blocking: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := kyvernopolicy.NewEngine(
				[]kyvernov1.PolicyInterface{
					policy(t, sprintf(erroringPolicy, testCase.action, testCase.spec)),
				},
				nil,
			)

			violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
			require.NoError(t, err)
			require.Len(t, violations, 1)
			assert.True(t, violations[0].Error)
			assert.Equal(t, testCase.blocking, violations[0].Blocking)
		})
	}
}

func TestEvaluate_ErrorInEnforceOverrideNamespaceBlocks(t *testing.T) {
	t.Parallel()

	overridden := policy(t, sprintf(erroringPolicy, "Audit", `  validationFailureActionOverrides:
  - action: Enforce
    namespaces: ["default"]`))
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{overridden}, nil)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(
		t,
		violations[0].Blocking,
		"an Enforce override places the policy on Kyverno's enforcing admission path",
	)
}

const prodOnlyPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team-label
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
          namespaceSelector:
            matchLabels:
              env: prod
    validate:
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`

func TestEvaluate_NamespaceSelectorUsesRenderedNamespaceLabels(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, prodOnlyPolicy)},
		[]map[string]any{
			namespace("shop", map[string]any{"env": "prod"}),
			namespace("sandbox", map[string]any{"env": "dev"}),
		},
	)

	selected, err := engine.Evaluate(t.Context(), configMap("shop", nil))
	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.True(t, selected[0].Blocking)
	assert.False(t, selected[0].Unsupported)

	unselected, err := engine.Evaluate(t.Context(), configMap("sandbox", nil))
	require.NoError(t, err)
	assert.Empty(t, unselected)
}

func TestEvaluate_NamespaceSelectorWithUnknownNamespaceIsUnsupported(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, prodOnlyPolicy), requireTeam(t, "Enforce")},
		nil,
	)

	violations, err := engine.Evaluate(t.Context(), configMap("elsewhere", nil))
	require.NoError(t, err)
	require.Len(t, violations, 2)

	byUnsupported := map[bool]kyvernopolicy.Violation{}
	for _, violation := range violations {
		byUnsupported[violation.Unsupported] = violation
	}

	unsupported, found := byUnsupported[true]
	require.True(t, found, "the selector policy must report that it could not be evaluated")
	assert.Equal(t, "require-team-label", unsupported.Policy)
	assert.False(t, unsupported.Blocking)
	assert.Contains(t, unsupported.Message, "elsewhere")

	evaluated, found := byUnsupported[false]
	require.True(t, found, "a policy without selectors must still be evaluated")
	assert.True(t, evaluated.Blocking)
}

func TestEvaluate_SelectorFailureActionOverrideUsesNamespaceLabels(t *testing.T) {
	t.Parallel()

	overridden := policy(t, `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team-label
spec:
  rules:
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      failureAction: Audit
      failureActionOverrides:
      - action: Enforce
        namespaceSelector:
          matchLabels:
            env: prod
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`)
	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{overridden},
		[]map[string]any{
			namespace("shop", map[string]any{"env": "prod"}),
			namespace("sandbox", map[string]any{"env": "dev"}),
		},
	)

	prod, err := engine.Evaluate(t.Context(), configMap("shop", nil))
	require.NoError(t, err)
	require.Len(t, prod, 1)
	assert.True(t, prod[0].Blocking)

	dev, err := engine.Evaluate(t.Context(), configMap("sandbox", nil))
	require.NoError(t, err)
	require.Len(t, dev, 1)
	assert.False(t, dev[0].Blocking)
}

const mixedActionPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: mixed-actions
spec:
  rules:
  - name: audit-owner
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      failureAction: Audit
      message: "label owner is recommended"
      pattern:
        metadata:
          labels:
            owner: "?*"
  - name: enforce-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      failureAction: Enforce
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`

func TestEvaluate_BlockingIsResolvedPerRule(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, mixedActionPolicy)},
		nil,
	)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 2)

	blocking := map[string]bool{}
	for _, violation := range violations {
		blocking[violation.Rule] = violation.Blocking
	}

	assert.False(t, blocking["audit-owner"], "an Audit rule never blocks, even beside an enforced failure")
	assert.True(t, blocking["enforce-team"], "an Enforce rule's failure blocks")
}

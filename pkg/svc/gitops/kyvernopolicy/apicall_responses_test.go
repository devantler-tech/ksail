package kyvernopolicy_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const apiCallResponsePolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-retirement-baseline
spec:
  failurePolicy: Fail
  validationFailureAction: Enforce
  rules:
  - name: check-retirement
    match:
      any:
      - resources:
          kinds: [ConfigMap]
    context:
    - name: retirement
      apiCall:
        method: GET
        urlPath: /api/v1/namespaces/arc-runners
        jmesPath: metadata.annotations.state
    validate:
      message: retirement state must be baseline
      deny:
        conditions:
          any:
          - key: "{{ retirement }}"
            operator: NotEquals
            value: baseline
`

// TestEvaluate_APICallFixtureUsesKyvernoJMESPath exercises Kyverno's real response projection.
func TestEvaluate_APICallFixtureUsesKyvernoJMESPath(t *testing.T) {
	t.Parallel()

	responses := kyvernopolicy.APICallResponses{
		{Method: "GET", URLPath: "/api/v1/namespaces/arc-runners"}: json.RawMessage(`{
			"metadata":{"annotations":{"state":"baseline"}}
		}`),
	}
	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, apiCallResponsePolicy)},
		nil,
		kyvernopolicy.WithAPICallResponses(responses),
	)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	assert.Empty(t, violations, "the real Kyverno loader must apply the apiCall jmesPath")
}

// TestEvaluate_APICallFixtureCanProducePolicyDenial keeps enforcing policy denials blocking.
func TestEvaluate_APICallFixtureCanProducePolicyDenial(t *testing.T) {
	t.Parallel()

	responses := kyvernopolicy.APICallResponses{
		{Method: "GET", URLPath: "/api/v1/namespaces/arc-runners"}: json.RawMessage(`{
			"metadata":{"annotations":{"state":"retired"}}
		}`),
	}
	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, apiCallResponsePolicy)},
		nil,
		kyvernopolicy.WithAPICallResponses(responses),
	)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.False(t, violations[0].Error)
	assert.True(t, violations[0].Blocking)
	assert.Contains(t, violations[0].Message, "retirement state must be baseline")
}

// TestEvaluate_APICallFixtureFailsClosedWhenResponseIsMissing rejects unmatched enforcing calls.
func TestEvaluate_APICallFixtureFailsClosedWhenResponseIsMissing(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, apiCallResponsePolicy)},
		nil,
		kyvernopolicy.WithAPICallResponses(kyvernopolicy.APICallResponses{}),
	)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Error)
	assert.True(t, violations[0].Blocking)
	assert.Contains(
		t,
		violations[0].Message,
		"no offline response for GET /api/v1/namespaces/arc-runners",
	)
}

// TestEvaluate_APICallFixtureMissingResponseCannotUseDefault prevents defaults from hiding misses.
func TestEvaluate_APICallFixtureMissingResponseCannotUseDefault(t *testing.T) {
	t.Parallel()

	policyWithDefault := strings.Replace(apiCallResponsePolicy,
		"jmesPath: metadata.annotations.state",
		"jmesPath: metadata.annotations.state\n        default: baseline", 1)
	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, policyWithDefault)},
		nil,
		kyvernopolicy.WithAPICallResponses(kyvernopolicy.APICallResponses{}),
	)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1, "a satisfying policy default cannot replace a missing fixture")
	assert.True(t, violations[0].Error)
	assert.True(t, violations[0].Blocking)
	assert.Contains(
		t,
		violations[0].Message,
		"no offline response for GET /api/v1/namespaces/arc-runners",
	)
}

// Method spellings normalize to one request, so two responses for GET and get
// are ambiguous. Evaluation fails closed rather than using either one.
func TestEvaluate_APICallFixtureRejectsResponsesThatNormalizeToOneRequest(t *testing.T) {
	t.Parallel()

	responses := kyvernopolicy.APICallResponses{
		{Method: "GET", URLPath: "/api/v1/namespaces/arc-runners"}: json.RawMessage(`{
			"metadata":{"annotations":{"state":"baseline"}}
		}`),
		{Method: "get", URLPath: "/api/v1/namespaces/arc-runners"}: json.RawMessage(`{
			"metadata":{"annotations":{"state":"retired"}}
		}`),
	}
	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, apiCallResponsePolicy)},
		nil,
		kyvernopolicy.WithAPICallResponses(responses),
	)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Error)
	assert.True(t, violations[0].Blocking)
	assert.Contains(
		t,
		violations[0].Message,
		"duplicate offline response for GET /api/v1/namespaces/arc-runners",
	)
}

type apiCallFixtureScopeCase struct {
	name        string
	namespaced  bool
	path        string
	state       string
	wantError   bool
	wantMessage string
}

func apiCallFixtureScopeCases() []apiCallFixtureScopeCase {
	return []apiCallFixtureScopeCase{
		{
			name: "cluster policy can read another namespace",
			path: "/api/v1/namespaces/team-b/configmaps/settings", state: "baseline",
		},
		{
			name: "namespaced policy can read its own namespace", namespaced: true,
			path: "/api/v1/namespaces/team-a/configmaps/settings", state: "baseline",
		},
		{
			name: "namespaced denial preserves policy identity", namespaced: true,
			path: "/api/v1/namespaces/team-a/configmaps/settings", state: "retired",
			wantMessage: "retirement state must be baseline",
		},
		{
			name: "fixture cannot bypass another namespace", namespaced: true,
			path: "/api/v1/namespaces/team-b/configmaps/settings", state: "baseline",
			wantError: true, wantMessage: "policy namespace team-a",
		},
		{
			name: "fixture cannot bypass cluster scope", namespaced: true,
			path: "/api/v1/namespaces/team-a", state: "baseline",
			wantError: true, wantMessage: "namespaced policies cannot access",
		},
	}
}

func TestEvaluate_APICallFixturePreservesPolicyScope(t *testing.T) {
	t.Parallel()

	for _, test := range apiCallFixtureScopeCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			source := strings.Replace(apiCallResponsePolicy,
				"/api/v1/namespaces/arc-runners", test.path, 1)
			policyName := "require-retirement-baseline"

			if test.namespaced {
				source = strings.Replace(source, "kind: ClusterPolicy", "kind: Policy", 1)
				source = strings.Replace(source, "  name: "+policyName,
					"  name: "+policyName+"\n  namespace: team-a", 1)
				policyName = "team-a/" + policyName
			}

			response, err := json.Marshal(map[string]any{
				"metadata": map[string]any{"annotations": map[string]string{"state": test.state}},
			})
			require.NoError(t, err)
			engine := kyvernopolicy.NewEngine(
				[]kyvernov1.PolicyInterface{policy(t, source)}, nil,
				kyvernopolicy.WithAPICallResponses(kyvernopolicy.APICallResponses{
					{Method: "GET", URLPath: test.path}: response,
				}),
			)

			violations, err := engine.Evaluate(t.Context(), configMap("team-a", nil))
			require.NoError(t, err)

			if test.wantMessage == "" {
				assert.Empty(t, violations)

				return
			}

			require.Len(t, violations, 1)
			assert.Equal(t, policyName, violations[0].Policy)
			assert.Equal(t, "check-retirement", violations[0].Rule)
			assert.Equal(t, test.wantError, violations[0].Error)
			assert.True(t, violations[0].Blocking)
			assert.Contains(t, violations[0].Message, test.wantMessage)
		})
	}
}

func TestEvaluate_APICallFixtureDelegatesVariableContext(t *testing.T) {
	t.Parallel()

	source := strings.Replace(apiCallResponsePolicy, `      apiCall:
        method: GET
        urlPath: /api/v1/namespaces/arc-runners
        jmesPath: metadata.annotations.state`, `      variable:
        jmesPath: request.object.metadata.labels.state`, 1)

	for _, state := range []string{"baseline", "retired"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			engine := kyvernopolicy.NewEngine(
				[]kyvernov1.PolicyInterface{policy(t, source)}, nil,
				kyvernopolicy.WithAPICallResponses(kyvernopolicy.APICallResponses{}),
			)
			violations, err := engine.Evaluate(t.Context(), configMap("default", map[string]any{
				"state": state,
			}))
			require.NoError(t, err)

			if state == "baseline" {
				assert.Empty(t, violations)

				return
			}

			require.Len(t, violations, 1)
			assert.Equal(t, "require-retirement-baseline", violations[0].Policy)
			assert.False(t, violations[0].Error)
			assert.True(t, violations[0].Blocking)
			assert.Contains(t, violations[0].Message, "retirement state must be baseline")
		})
	}
}

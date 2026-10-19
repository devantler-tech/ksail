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

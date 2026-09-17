package kyvernopolicy_test

import (
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const requireTeamLabel = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team-label
spec:
  validationFailureAction: %s
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
`

func decodeYAML(t *testing.T, content string) map[string]any {
	t.Helper()

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(content), &doc))

	return doc
}

func policy(t *testing.T, content string) kyvernov1.PolicyInterface {
	t.Helper()

	doc := decodeYAML(t, content)
	require.True(t, kyvernopolicy.IsPolicy(doc))

	decoded, err := kyvernopolicy.DecodePolicy(doc)
	require.NoError(t, err)

	return decoded
}

func configMap(namespace string, labels map[string]any) map[string]any {
	metadata := map[string]any{"name": "settings", "namespace": namespace}
	if labels != nil {
		metadata["labels"] = labels
	}

	return map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": metadata}
}

func requireTeam(t *testing.T, action string) kyvernov1.PolicyInterface {
	t.Helper()

	return policy(t, sprintf(requireTeamLabel, action))
}

func TestEvaluate_EnforceFailureIsBlocking(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{requireTeam(t, "Enforce")})

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "require-team-label", violations[0].Policy)
	assert.Equal(t, "check-team", violations[0].Rule)
	assert.Contains(t, violations[0].Message, "label team is required")
	assert.True(t, violations[0].Blocking)
	assert.False(t, violations[0].Error)
}

func TestEvaluate_CompliantDocumentPasses(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{requireTeam(t, "Enforce")})

	violations, err := engine.Evaluate(t.Context(), configMap("default", map[string]any{"team": "platform"}))
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluate_AuditFailureIsNotBlocking(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{requireTeam(t, "Audit")})

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.False(t, violations[0].Blocking)
}

func TestEvaluate_UnmatchedKindIsIgnored(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{requireTeam(t, "Enforce")})
	secret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "token", "namespace": "default"},
	}

	violations, err := engine.Evaluate(t.Context(), secret)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluate_NamespacedPolicyAppliesOnlyToItsNamespace(t *testing.T) {
	t.Parallel()

	namespaced := policy(t, `
apiVersion: kyverno.io/v1
kind: Policy
metadata:
  name: require-team-label
  namespace: team-a
spec:
  validationFailureAction: Enforce
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
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{namespaced})

	inside, err := engine.Evaluate(t.Context(), configMap("team-a", nil))
	require.NoError(t, err)
	require.Len(t, inside, 1)
	assert.Equal(t, "team-a/require-team-label", inside[0].Policy)

	outside, err := engine.Evaluate(t.Context(), configMap("team-b", nil))
	require.NoError(t, err)
	assert.Empty(t, outside)
}

func TestEvaluate_PolicyWithoutValidateRulesIsIgnored(t *testing.T) {
	t.Parallel()

	mutating := policy(t, `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: add-team-label
spec:
  rules:
  - name: add-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    mutate:
      patchStrategicMerge:
        metadata:
          labels:
            team: platform
`)
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{mutating})

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestIsPolicy(t *testing.T) {
	t.Parallel()

	assert.True(t, kyvernopolicy.IsPolicy(map[string]any{"apiVersion": "kyverno.io/v1", "kind": "ClusterPolicy"}))
	assert.True(t, kyvernopolicy.IsPolicy(map[string]any{"apiVersion": "kyverno.io/v1", "kind": "Policy"}))
	assert.False(t, kyvernopolicy.IsPolicy(map[string]any{"apiVersion": "policies.kyverno.io/v1", "kind": "ValidatingPolicy"}))
	assert.False(t, kyvernopolicy.IsPolicy(configMap("default", nil)))
}

func TestDecodePolicy_RejectsNonPolicy(t *testing.T) {
	t.Parallel()

	_, err := kyvernopolicy.DecodePolicy(configMap("default", nil))
	require.Error(t, err)
}

func TestDecodePolicy_RejectsUnknownField(t *testing.T) {
	t.Parallel()

	doc := decodeYAML(t, sprintf(requireTeamLabel, "Enforce"))
	spec, ok := doc["spec"].(map[string]any)
	require.True(t, ok)
	spec["validationFailureActon"] = "Enforce"

	_, err := kyvernopolicy.DecodePolicy(doc)
	require.ErrorContains(t, err, "require-team-label")
}

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

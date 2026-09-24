package kyvernopolicy_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type namespaceEvaluator interface {
	Evaluate(ctx context.Context, doc map[string]any) ([]kyvernopolicy.Violation, error)
}

func namespaceEvaluators(
	t *testing.T,
	selector *metav1.LabelSelector,
) map[string]namespaceEvaluator {
	t.Helper()

	classic := requireTeam(t, "Enforce")
	classic.GetSpec().Rules[0].MatchResources.Any[0].NamespaceSelector = selector
	cel := requireTeamCEL(t, "v1", "Deny")
	cel.GetValidatingPolicySpec().MatchConstraints.NamespaceSelector = selector

	return map[string]namespaceEvaluator{
		"classic": kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{classic}, nil),
		"CEL":     celEngine(t, nil, cel),
	}
}

func TestNamespaceSelectorsUseOnlyKnownLabels(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		selector *metav1.LabelSelector
		blocking bool
		unknown  bool
	}{
		{name: "automatic name matches", selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
		}, blocking: true},
		{name: "automatic name excludes", selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": "flux-system"},
		}},
		{name: "custom labels remain unknown", selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app.kubernetes.io/managed-by": "ksail"},
		}, unknown: true},
		{name: "known name does not prove custom labels", selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": "kube-system", "team": "platform",
			},
		}, unknown: true},
		{name: "known mismatch decides a conjunction", selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": "flux-system", "team": "platform",
			},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for name, engine := range namespaceEvaluators(t, test.selector) {
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					violations, err := engine.Evaluate(t.Context(), configMap("kube-system", nil))
					require.NoError(t, err)

					if !test.blocking && !test.unknown {
						assert.Empty(t, violations)

						return
					}

					require.Len(t, violations, 1)
					assert.Equal(t, test.blocking, violations[0].Blocking)
					assert.Equal(t, test.unknown, violations[0].Unsupported)
				})
			}
		})
	}
}

func TestNamespaceSelectorExpressionsPreserveUnknownKeys(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		key      string
		operator metav1.LabelSelectorOperator
		values   []string
		blocking bool
		unknown  bool
	}{
		{"name in", "kubernetes.io/metadata.name", metav1.LabelSelectorOpIn, []string{"kube-system"}, true, false},
		{"name not in", "kubernetes.io/metadata.name", metav1.LabelSelectorOpNotIn, []string{"kube-system"}, false, false},
		{"name exists", "kubernetes.io/metadata.name", metav1.LabelSelectorOpExists, nil, true, false},
		{"name cannot be absent", "kubernetes.io/metadata.name", metav1.LabelSelectorOpDoesNotExist, nil, false, false},
		{"unknown absence", "team", metav1.LabelSelectorOpDoesNotExist, nil, false, true},
		{"unknown negative match", "team", metav1.LabelSelectorOpNotIn, []string{"platform"}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			selector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: test.key, Operator: test.operator, Values: test.values,
			}}}
			for name, engine := range namespaceEvaluators(t, selector) {
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					violations, err := engine.Evaluate(t.Context(), configMap("kube-system", nil))
					require.NoError(t, err)

					if !test.blocking && !test.unknown {
						assert.Empty(t, violations)

						return
					}

					require.Len(t, violations, 1)
					assert.Equal(t, test.blocking, violations[0].Blocking)
					assert.Equal(t, test.unknown, violations[0].Unsupported)
				})
			}
		})
	}
}

func systemNamespaceSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{corev1.LabelMetadataName: "kube-system"},
	}
}

func TestNamespaceSelectorsKeepKnownRulesBesideUnknownRules(t *testing.T) {
	t.Parallel()

	pol := policy(t, mixedSelectorPolicy)
	pol.GetSpec().Rules[1].MatchResources.Any[0].NamespaceSelector = systemNamespaceSelector()
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{pol}, nil)
	violations, err := engine.Evaluate(t.Context(), configMap("kube-system", nil))
	require.NoError(t, err)
	require.Len(t, violations, 2)
	assert.True(t, violations[0].Unsupported)
	assert.True(t, violations[1].Blocking)
	assert.Equal(t, "owner-everywhere", violations[1].Rule)
}

func TestNamespaceSelectorsPreserveApplyOneUncertainty(t *testing.T) {
	t.Parallel()

	pol := policy(t, mixedSelectorPolicy)
	applyOne := kyvernov1.ApplyOne
	pol.GetSpec().ApplyRules = &applyOne
	pol.GetSpec().Rules[1].MatchResources.Any[0].NamespaceSelector = systemNamespaceSelector()
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{pol}, nil)
	violations, err := engine.Evaluate(t.Context(), configMap("kube-system", nil))
	require.NoError(t, err)
	require.Len(t, violations, 2)
	assert.True(t, violations[0].Unsupported)
	assert.True(t, violations[1].Unsupported)
	assert.Contains(t, violations[1].Message, "applyRules: One")
}

func TestNamespaceSelectorsResolveExclusions(t *testing.T) {
	t.Parallel()

	for name, selector := range map[string]*metav1.LabelSelector{
		"known":   systemNamespaceSelector(),
		"unknown": {MatchLabels: map[string]string{"team": "platform"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pol := requireTeam(t, "Enforce")
			pol.GetSpec().Rules[0].ExcludeResources = &kyvernov1.MatchResources{
				Any: kyvernov1.ResourceFilters{{ResourceDescription: kyvernov1.ResourceDescription{
					NamespaceSelector: selector,
				}}},
			}
			engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{pol}, nil)
			violations, err := engine.Evaluate(t.Context(), configMap("kube-system", nil))
			require.NoError(t, err)

			if name == "known" {
				assert.Empty(t, violations)
			} else {
				require.Len(t, violations, 1)
				assert.True(t, violations[0].Unsupported)
			}
		})
	}
}

func TestNamespaceSelectorsResolveFailureActionOverrides(t *testing.T) {
	t.Parallel()

	for name, selector := range map[string]*metav1.LabelSelector{
		"known":   systemNamespaceSelector(),
		"unknown": {MatchLabels: map[string]string{"team": "platform"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pol := requireTeam(t, "Enforce")
			pol.GetSpec().Rules[0].Validation.FailureActionOverrides = []kyvernov1.ValidationFailureActionOverride{
				{
					Action: kyvernov1.Audit, NamespaceSelector: selector,
				},
			}
			engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{pol}, nil)
			violations, err := engine.Evaluate(t.Context(), configMap("kube-system", nil))
			require.NoError(t, err)
			require.Len(t, violations, 1)
			assert.False(t, violations[0].Blocking)
			assert.Equal(t, name == "unknown", violations[0].Unsupported)
		})
	}
}

func TestNamespaceNameDoesNotInventNamespaceObject(t *testing.T) {
	t.Parallel()

	pol := validatingPolicy(t, readsNamespaceObject)
	pol.GetValidatingPolicySpec().MatchConstraints.NamespaceSelector = systemNamespaceSelector()
	violations, err := celEngine(t, nil, pol).Evaluate(t.Context(), configMap("kube-system", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.True(t, violations[0].Unsupported)
	assert.Contains(t, violations[0].Message, "namespaceObject")
}

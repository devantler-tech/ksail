package kyvernopolicy

import (
	"slices"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// namespaceSelectorKnown reports whether the selector's result is determined by
// Kubernetes' immutable namespace-name label alone. Other labels are unknown,
// not absent. A false name requirement decides an AND selector even when other
// requirements are unknown; a true name requirement does not.
func namespaceSelectorKnown(selector *metav1.LabelSelector, namespace string) bool {
	if selector == nil {
		return true
	}

	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}

	requirements, selectable := parsed.Requirements()
	if !selectable {
		return false
	}

	known := labels.Set{corev1.LabelMetadataName: namespace}
	unknown := false

	for _, requirement := range requirements {
		if namespace == "" || requirement.Key() != corev1.LabelMetadataName {
			unknown = true

			continue
		}

		if !requirement.Matches(known) {
			return true
		}
	}

	return !unknown
}

// namespaceSelectorExcludes reports whether the namespace-name label alone
// decides the selector as not matching, so the policy cannot select documents
// in that namespace.
func namespaceSelectorExcludes(selector *metav1.LabelSelector, namespace string) bool {
	if selector == nil || namespace == "" {
		return false
	}

	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}

	requirements, _ := parsed.Requirements()
	known := labels.Set{corev1.LabelMetadataName: namespace}

	return slices.ContainsFunc(requirements, func(requirement labels.Requirement) bool {
		return requirement.Key() == corev1.LabelMetadataName && !requirement.Matches(known)
	})
}

// policyNamespaceSelectorsKnown requires every selector to be decidable before
// passing partial labels to Kyverno, including exclusions and action overrides.
func policyNamespaceSelectorsKnown(policy kyvernov1.PolicyInterface, namespace string) bool {
	spec := policy.GetSpec()
	selectors := overrideNamespaceSelectors(spec.ValidationFailureActionOverrides)

	for _, rule := range spec.Rules {
		selectors = append(selectors, matchNamespaceSelectors(&rule.MatchResources)...)
		selectors = append(selectors, matchNamespaceSelectors(rule.ExcludeResources)...)

		if rule.Validation != nil {
			selectors = append(selectors,
				overrideNamespaceSelectors(rule.Validation.FailureActionOverrides)...)
		}
	}

	return !slices.ContainsFunc(selectors, func(selector *metav1.LabelSelector) bool {
		return !namespaceSelectorKnown(selector, namespace)
	})
}

func matchNamespaceSelectors(match *kyvernov1.MatchResources) []*metav1.LabelSelector {
	if match == nil {
		return nil
	}

	selectors := []*metav1.LabelSelector{match.NamespaceSelector}
	for _, filter := range match.Any {
		selectors = append(selectors, filter.NamespaceSelector)
	}

	for _, filter := range match.All {
		selectors = append(selectors, filter.NamespaceSelector)
	}

	return selectors
}

func overrideNamespaceSelectors(
	overrides []kyvernov1.ValidationFailureActionOverride,
) []*metav1.LabelSelector {
	selectors := make([]*metav1.LabelSelector, 0, len(overrides))
	for _, override := range overrides {
		selectors = append(selectors, override.NamespaceSelector)
	}

	return selectors
}

// namespaceForMatching supplies only the label guaranteed by Kubernetes. It is
// not a rendered namespaceObject: expressions reading that object remain
// unsupported until its Namespace is present in the rendered source.
func namespaceForMatching(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: name, Labels: map[string]string{corev1.LabelMetadataName: name},
	}}
}

package calicoinstaller

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const multiMutationPolicyCount = 2

func verifiedOperatorSpecDigest(
	desired, live *unstructured.Unstructured,
) (string, error) {
	wanted, err := prerequisiteSpecDigest(desired)
	if err != nil {
		return "", err
	}

	actual, err := prerequisiteSpecDigest(live)
	if err != nil {
		return "", err
	}

	if wanted == actual {
		return actual, nil
	}

	matches, err := matchesOperatorDocumentSerialization(desired, actual)
	if err != nil {
		return "", err
	}

	if !matches {
		return "", prerequisiteError("Calico operator prerequisite content differs from the chart")
	}

	return actual, nil
}

// Tigera operator v1.42.6/v1.44.0 trims each admission YAML document before
// decoding. In these audited policies that removes one terminal LF from the
// final JSONPatch expression. Recognize only that desired-content variant;
// the observed digest remains exact for every later provenance revalidation.
func matchesOperatorDocumentSerialization(
	desired *unstructured.Unstructured,
	actualDigest string,
) (bool, error) {
	if desired.GroupVersionKind().Group != admissionRegistrationGroup ||
		desired.GetKind() != mutatingAdmissionPolicyKind {
		return false, nil
	}

	mutations, err := operatorDocumentMutations(desired)
	if err != nil {
		return false, err
	}

	if len(mutations) == 0 || !trimOperatorTerminalExpression(mutations[len(mutations)-1]) {
		return false, nil
	}

	variant := desired.DeepCopy()

	err = unstructured.SetNestedSlice(variant.Object, mutations, "spec", "mutations")
	if err != nil {
		return false, fmt.Errorf("encode Calico operator serialization variant: %w", err)
	}

	digest, err := prerequisiteSpecDigest(variant)
	if err != nil {
		return false, err
	}

	return digest == actualDigest, nil
}

func operatorDocumentMutations(desired *unstructured.Unstructured) ([]any, error) {
	count := operatorDocumentMutationCount(desired.GetName())
	if count == 0 {
		return nil, nil
	}

	mutations, found, err := unstructured.NestedSlice(desired.Object, "spec", "mutations")
	if err != nil {
		return nil, fmt.Errorf("read Calico operator mutations: %w", err)
	}

	if !found || len(mutations) != count {
		return nil, nil
	}

	return mutations, nil
}

func trimOperatorTerminalExpression(value any) bool {
	mutation, isMutation := value.(map[string]any)
	if !isMutation || mutation["patchType"] != "JSONPatch" {
		return false
	}

	patch, isPatch := mutation["jsonPatch"].(map[string]any)
	if !isPatch {
		return false
	}

	expression, isExpression := patch["expression"].(string)
	if !isExpression || !strings.HasSuffix(expression, "]\n") {
		return false
	}

	patch["expression"] = strings.TrimSuffix(expression, "\n")

	return true
}

func operatorDocumentMutationCount(name string) int {
	switch name {
	case "policytypes.policy.projectcalico.org":
		return 1
	case "tierlabel.policy.projectcalico.org", "ippool.policy.projectcalico.org":
		return multiMutationPolicyCount
	default:
		return 0
	}
}

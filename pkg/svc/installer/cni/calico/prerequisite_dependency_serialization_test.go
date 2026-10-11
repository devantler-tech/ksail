package calicoinstaller_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// These specimens come from Tigera operator v1.44.0 pkg/imports/admission/calico.
// Published Calico v3.33.0 prerequisite specs match them; the operator trims each
// document before YAML decoding, unlike the prerequisite chart renderer.
func serializationPolicy(t *testing.T, name string, trim bool) *unstructured.Unstructured {
	t.Helper()

	base := filepath.Join("testdata", "operator-admission")
	data, err := fsutil.ReadFileSafe(base, filepath.Join(base, name+".yaml"))
	require.NoError(t, err)

	if trim {
		data = bytes.TrimSpace(data)
	}

	object := &unstructured.Unstructured{}
	require.NoError(t, yaml.Unmarshal(data, &object.Object))

	return object
}

func serializationValue[T any](t *testing.T, value any) T {
	t.Helper()

	typed, valid := value.(T)
	require.True(t, valid)

	return typed
}

func serializationSpec(t *testing.T, object *unstructured.Unstructured) map[string]any {
	t.Helper()

	return serializationValue[map[string]any](t, object.Object["spec"])
}

func serializationMutations(t *testing.T, object *unstructured.Unstructured) []any {
	t.Helper()

	return serializationValue[[]any](t, serializationSpec(t, object)["mutations"])
}

func serializationPatch(t *testing.T, object *unstructured.Unstructured) map[string]any {
	t.Helper()
	mutations := serializationMutations(t, object)
	require.NotEmpty(t, mutations)
	mutation := serializationValue[map[string]any](t, mutations[0])

	return serializationValue[map[string]any](t, mutation["jsonPatch"])
}

func TestCalicoRecognizesAuditedOperatorDocumentSerialization(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"networkpolicy", "tierlabel", "ippool"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			desired := serializationPolicy(t, name, false)
			live := serializationPolicy(t, name, true)
			original := desired.DeepCopy()
			wantedDigest, err := calicoinstaller.PrerequisiteSpecDigestForTest(desired)
			require.NoError(t, err)
			actualDigest, err := calicoinstaller.PrerequisiteSpecDigestForTest(live)
			require.NoError(t, err)
			require.NotEqual(t, wantedDigest, actualDigest,
				"observed fingerprints must retain the parser's exact content")

			canonical, _, err := calicoinstaller.ObserveOperatorPrerequisiteForTest(
				t,
				desired,
				desired,
			)
			require.NoError(t, err, "unchanged chart content must still be recognized")
			require.Equal(t, wantedDigest, canonical)

			recorded, revalidate, err := calicoinstaller.ObserveOperatorPrerequisiteForTest(
				t,
				desired,
				live,
			)
			require.NoError(t, err)
			require.Equal(t, actualDigest, recorded)
			require.Equal(t, original, desired, "recognition must not alter desired content")
			require.NoError(t, revalidate(serializationSpec(t, live)))
			require.ErrorContains(
				t,
				revalidate(serializationSpec(t, desired)),
				"identity or content changed",
				"even restoring the chart LF changes the recorded content",
			)
		})
	}
}

func TestCalicoRejectsOtherOperatorSerializationDifferences(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*testing.T, *unstructured.Unstructured){
		"unknown policy": func(_ *testing.T, object *unstructured.Unstructured) {
			object.SetName("unknown.policy.projectcalico.org")
		},
		"extra mutation": func(t *testing.T, object *unstructured.Unstructured) {
			t.Helper()
			mutations := serializationMutations(t, object)
			serializationSpec(t, object)["mutations"] = append(mutations, mutations[0])
		},
		"changed failure policy": func(t *testing.T, object *unstructured.Unstructured) {
			t.Helper()
			serializationSpec(t, object)["failurePolicy"] = "Ignore"
		},
		"other policy kind": func(_ *testing.T, object *unstructured.Unstructured) {
			object.SetKind("ValidatingAdmissionPolicy")
		},
		"other patch type": func(t *testing.T, object *unstructured.Unstructured) {
			t.Helper()
			mutation := serializationValue[map[string]any](t, serializationMutations(t, object)[0])
			mutation["patchType"] = "ApplyConfiguration"
		},
	}
	for name, change := range map[string]func(string) string{
		"extra LF":          func(value string) string { return value + "\n\n" },
		"CRLF":              func(value string) string { return value + "\r\n" },
		"leading LF":        func(value string) string { return "\n" + value },
		"trailing space":    func(value string) string { return value + " " },
		"changed operation": func(value string) string { return strings.Replace(value, `"add"`, `"remove"`, 1) },
		"changed literal":   func(value string) string { return strings.Replace(value, "/spec/types", "/spec/other", 1) },
	} {
		cases[name] = func(t *testing.T, object *unstructured.Unstructured) {
			t.Helper()

			patch := serializationPatch(t, object)
			expression := serializationValue[string](t, patch["expression"])
			patch["expression"] = change(expression)
		}
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			desired := serializationPolicy(t, "networkpolicy", false)
			live := serializationPolicy(t, "networkpolicy", true)
			mutate(t, live)

			if name == "unknown policy" || name == "extra mutation" ||
				name == "other policy kind" || name == "other patch type" {
				mutate(t, desired)
			}

			_, _, err := calicoinstaller.ObserveOperatorPrerequisiteForTest(t, desired, live)
			require.Error(t, err)
		})
	}
}

func TestCalicoRejectsNonterminalOperatorExpressionTrimming(t *testing.T) {
	t.Parallel()
	desired := serializationPolicy(t, "tierlabel", false)
	live := desired.DeepCopy()
	patch := serializationPatch(t, live)
	expression := serializationValue[string](t, patch["expression"])
	require.True(t, strings.HasSuffix(expression, "\n"))
	patch["expression"] = strings.TrimSuffix(expression, "\n")
	_, _, err := calicoinstaller.ObserveOperatorPrerequisiteForTest(t, desired, live)
	require.Error(t, err, "the parser changes only the document's terminal scalar")
}

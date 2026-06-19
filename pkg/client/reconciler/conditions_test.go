package reconciler_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// condMap builds a status-condition entry as it appears in an unstructured CR.
func condMap(condType, status, reason, message string) map[string]any {
	return map[string]any{
		"type":    condType,
		"status":  status,
		"reason":  reason,
		"message": message,
	}
}

// objWithConditions wraps a status.conditions slice in an unstructured object.
// Slice entries must be JSON-valid types because ParseConditions reads them via
// unstructured.NestedSlice, which deep-copies and panics on non-JSON Go types.
func objWithConditions(conditions []any) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"status": map[string]any{
				"conditions": conditions,
			},
		},
	}
}

// TestParseConditions_NoStatus tests that an object without a status.conditions
// path yields nil.
func TestParseConditions_NoStatus(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{Object: map[string]any{}}

	assert.Nil(t, reconciler.ParseConditions(obj))
}

// TestParseConditions_EmptySlice tests that a present-but-empty conditions slice
// yields nil rather than an empty slice.
func TestParseConditions_EmptySlice(t *testing.T) {
	t.Parallel()

	obj := objWithConditions([]any{})

	assert.Nil(t, reconciler.ParseConditions(obj))
}

// TestParseConditions_AllValid tests that every valid condition map is parsed
// and that the original order is preserved.
func TestParseConditions_AllValid(t *testing.T) {
	t.Parallel()

	obj := objWithConditions([]any{
		condMap("Ready", "True", "ReconciliationSucceeded", "applied revision main"),
		condMap("Stalled", "False", "", ""),
	})

	got := reconciler.ParseConditions(obj)

	require.Len(t, got, 2)
	assert.Equal(t, reconciler.Condition{
		Type:    "Ready",
		Status:  "True",
		Reason:  "ReconciliationSucceeded",
		Message: "applied revision main",
	}, got[0])
	assert.Equal(t, reconciler.Condition{Type: "Stalled", Status: "False"}, got[1])
}

// TestParseConditions_SkipsNonMapEntries tests that non-map entries are silently
// skipped while valid maps are still returned in order.
func TestParseConditions_SkipsNonMapEntries(t *testing.T) {
	t.Parallel()

	obj := objWithConditions([]any{
		"not-a-map",
		condMap("Ready", "True", "", ""),
		int64(42),
		nil,
		condMap("Healthy", "True", "", ""),
	})

	got := reconciler.ParseConditions(obj)

	require.Len(t, got, 2)
	assert.Equal(t, "Ready", got[0].Type)
	assert.Equal(t, "Healthy", got[1].Type)
}

// runParseConditionCases runs a ParseCondition table and asserts the parsed
// condition and ok flag for each entry.
func runParseConditionCases(t *testing.T, cases []struct {
	name     string
	entry    any
	wantCond reconciler.Condition
	wantOK   bool
},
) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cond, ok := reconciler.ParseCondition(testCase.entry)

			assert.Equal(t, testCase.wantOK, ok)
			assert.Equal(t, testCase.wantCond, cond)
		})
	}
}

// TestParseCondition_NonMapEntries tests that entries which are not condition
// maps are rejected without panicking.
func TestParseCondition_NonMapEntries(t *testing.T) {
	t.Parallel()

	runParseConditionCases(t, []struct {
		name     string
		entry    any
		wantCond reconciler.Condition
		wantOK   bool
	}{
		{"string entry", "Ready", reconciler.Condition{}, false},
		{"integer entry", 42, reconciler.Condition{}, false},
		{"nil entry", nil, reconciler.Condition{}, false},
	})
}

// TestParseCondition_MapEntries tests parsing of full, partial, empty, and
// wrong-typed condition maps.
func TestParseCondition_MapEntries(t *testing.T) {
	t.Parallel()

	runParseConditionCases(t, []struct {
		name     string
		entry    any
		wantCond reconciler.Condition
		wantOK   bool
	}{
		{
			"full map populates every field",
			condMap("Ready", "True", "ReconciliationSucceeded", "all good"),
			reconciler.Condition{
				Type:    "Ready",
				Status:  "True",
				Reason:  "ReconciliationSucceeded",
				Message: "all good",
			},
			true,
		},
		{
			"partial map defaults missing fields to empty",
			map[string]any{"type": "Ready", "status": "True"},
			reconciler.Condition{Type: "Ready", Status: "True"},
			true,
		},
		{
			"empty map yields a zero condition that is still valid",
			map[string]any{},
			reconciler.Condition{},
			true,
		},
		{
			"non-string field types are ignored without panicking",
			map[string]any{"type": 7, "status": true, "reason": "Stalled"},
			reconciler.Condition{Reason: "Stalled"},
			true,
		},
	})
}

package celrules_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/celrules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocumentIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		object   map[string]any
		expected string
	}{
		{
			name:     "kind and name",
			object:   map[string]any{"kind": "Service", "metadata": map[string]any{"name": "web"}},
			expected: "Service/web",
		},
		{
			name: "kind namespace name",
			object: map[string]any{
				"kind":     "Deployment",
				"metadata": map[string]any{"name": "web", "namespace": "prod"},
			},
			expected: "Deployment/prod/web",
		},
		{
			name:     "missing kind and name",
			object:   map[string]any{},
			expected: "Unknown/<unnamed>",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, celrules.DocumentIdentity(testCase.object))
		})
	}
}

const serviceDocument = `kind: Service
metadata:
  name: web
`

func TestParseDocument(t *testing.T) {
	t.Parallel()

	object, identity, err := celrules.ParseDocument([]byte(serviceDocument))

	require.NoError(t, err)
	assert.Equal(t, "Service/web", identity)
	assert.Equal(t, "Service", object["kind"])
}

func TestParseDocument_Blank(t *testing.T) {
	t.Parallel()

	for _, blank := range []string{"", "   \n", "---\n", "null\n"} {
		_, _, err := celrules.ParseDocument([]byte(blank))
		require.Error(t, err, "blank document %q must error", blank)
	}
}

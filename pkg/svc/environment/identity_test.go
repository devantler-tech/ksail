package environment_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scaffoldedBaseConfig = `# yaml-language-server: $schema=https://example.invalid/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  workload:
    kustomizationFile: clusters/staging
`

func TestMaterializeIdentityWritesAnUnsetNameAndContext(t *testing.T) {
	t.Parallel()

	got, err := environment.MaterializeIdentity(
		scaffoldedBaseConfig,
		environment.Identity{Name: "staging", Context: "kind-staging"},
	)
	require.NoError(t, err)

	assert.Equal(
		t,
		`# yaml-language-server: $schema=https://example.invalid/ksail-config.schema.json
apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: staging
spec:
  workload:
    kustomizationFile: clusters/staging
  cluster:
    connection:
      context: kind-staging
`,
		got,
	)
}

func TestMaterializeIdentityKeepsDeclaredFields(t *testing.T) {
	t.Parallel()

	const declared = `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: hand-picked
spec:
  cluster:
    connection:
      context: my-context
`

	got, err := environment.MaterializeIdentity(
		declared,
		environment.Identity{Name: "staging", Context: "kind-staging"},
	)
	require.NoError(t, err)
	assert.Equal(t, declared, got, "a declared identity must be returned byte-for-byte")
}

func TestMaterializeIdentityFillsAnEmptyValue(t *testing.T) {
	t.Parallel()

	got, err := environment.MaterializeIdentity(
		"metadata:\n  name: \"\"\nspec: {}\n",
		environment.Identity{Name: "staging"},
	)
	require.NoError(t, err)
	assert.Contains(t, got, "name: staging")
	assert.NotContains(t, got, "context:", "an empty Context is not materialized")
}

func TestMaterializeIdentityRejectsANonMappingConfig(t *testing.T) {
	t.Parallel()

	_, err := environment.MaterializeIdentity(
		"- not\n- a mapping\n",
		environment.Identity{Name: "staging"},
	)
	require.ErrorIs(t, err, environment.ErrInvalidConfig)
}

func TestMaterializeIdentityFillsANullValue(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty intermediate": "metadata:\n  name: ~\nspec:\n",
		"null intermediate":  "metadata:\n  name: null\nspec: ~\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := environment.MaterializeIdentity(
				content,
				environment.Identity{Name: "staging", Context: "kind-staging"},
			)
			require.NoError(t, err)
			assert.Contains(t, got, "name: staging")
			assert.Contains(t, got, "context: kind-staging")
			assert.NotContains(t, got, "null")
			assert.NotContains(t, got, "~")
		})
	}
}

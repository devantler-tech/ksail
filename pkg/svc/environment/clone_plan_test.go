package environment_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cloneRootFieldsTemplate = `metadata:
  name: %s
  annotations: {name: local, provider: Docker, context: kind-local}
spec:
  cluster:
    provider: %s
    connection: {context: %s}
unrelated: {name: local, provider: Docker, context: kind-local}
`

func TestClonePlanResolvesPartialTargetsBeforeRootOnlyMutation(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, cluster, provider string
		target                  environment.CloneTarget
	}{
		{"empty target", "prod", "Kubernetes", environment.CloneTarget{}},
		{"provider only", "prod", "Hetzner", environment.CloneTarget{Provider: "Hetzner"}},
		{"name only", "explicit", "Kubernetes", environment.CloneTarget{
			Identity: environment.Identity{Name: "explicit"},
		}},
		{"explicit target", "explicit", "Hetzner", environment.CloneTarget{
			Identity: environment.Identity{Name: "explicit", Context: "fill-only"}, Provider: "Hetzner",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := fmt.Sprintf(cloneRootFieldsTemplate, "custom-source", "Docker", "kind-local")
			require.NoError(
				t,
				os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(source), 0o600),
			)

			rewrites := environment.DeriveConfigRewrites("local", "prod", "Kubernetes", "Docker")
			rewrites = append(rewrites, environment.Rewrite{
				Kind:  environment.MetaFieldValue,
				Field: "context",
				Old:   "kind-local",
				New:   "kind-prod",
			})
			path, wrote, err := environment.CloneEnvironmentConfigForTarget(root,
				"ksail.local.yaml", "ksail.prod.yaml", rewrites, testCase.target, false)
			require.NoError(t, err)
			require.True(t, wrote)
			assert.YAMLEq(t, fmt.Sprintf(cloneRootFieldsTemplate,
				testCase.cluster, testCase.provider, "kind-prod"), readClone(t, root, path))
			assert.Equal(t, source, readClone(t, root, "ksail.local.yaml"))
		})
	}
}

func TestClonePlanRejectsContextAliasMutationBeforeWriting(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"shared context": "metadata: {name: local}\nspec:\n  cluster:\n    connection:\n" +
			"      context: &shared kind-local\nsaved: *shared\n",
		"aliased connection": "metadata: {name: local}\ndefaults: &shared {context: kind-local}\n" +
			"spec:\n  cluster:\n    connection: *shared\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(
				t,
				os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(source), 0o600),
			)

			rewrites := []environment.Rewrite{
				{
					Kind:  environment.MetaFieldValue,
					Field: "context",
					Old:   "kind-local",
					New:   "kind-prod",
				},
			}
			_, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
				"ksail.local.yaml", "ksail.prod.yaml", rewrites, environment.CloneTarget{
					Identity: environment.Identity{Name: "prod", Context: "kind-prod"},
				})
			require.ErrorIs(t, err, environment.ErrInvalidConfig)
			assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"))
			assert.Equal(t, source, readClone(t, root, "ksail.local.yaml"))
		})
	}
}

func TestClonePlanRejectsMalformedFilteredRewrites(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"name", "provider", "context"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.local.yaml"),
				[]byte("metadata: {name: local}\nspec: {}\n"), 0o600))
			_, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
				"ksail.local.yaml", "ksail.prod.yaml", []environment.Rewrite{{
					Kind: environment.MetaFieldValue, Field: field, Old: "", New: "prod",
				}}, environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
			require.ErrorIs(t, err, environment.ErrInvalidRewrite)
			assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"))
		})
	}
}

func TestClonePlanResolvesOrderedRootRewrites(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, finalName string
		rewrites        []environment.Rewrite
		target          environment.CloneTarget
		conflict        bool
	}{
		{"chain", "staging", []environment.Rewrite{
			{Kind: environment.MetaFieldValue, Field: "name", Old: "local", New: "prod"},
			{Kind: environment.MetaFieldValue, Field: "name", Old: "prod", New: "staging"},
		}, environment.CloneTarget{}, false},
		{"conflict", "", []environment.Rewrite{
			{Kind: environment.MetaFieldValue, Field: "name", Old: "local", New: "prod"},
			{Kind: environment.MetaFieldValue, Field: "name", Old: "local", New: "staging"},
		}, environment.CloneTarget{}, true},
		{"explicit wins", "chosen", []environment.Rewrite{
			{Kind: environment.MetaFieldValue, Field: "name", Old: "local", New: "prod"},
			{Kind: environment.MetaFieldValue, Field: "name", Old: "local", New: "staging"},
		}, environment.CloneTarget{Identity: environment.Identity{Name: "chosen"}}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := fmt.Sprintf(cloneRootFieldsTemplate, "custom-source", "Docker", "kind-local")
			require.NoError(
				t,
				os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(source), 0o600),
			)

			clone, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
				"ksail.local.yaml", "ksail.prod.yaml", testCase.rewrites, testCase.target)
			if testCase.conflict {
				require.ErrorIs(t, err, environment.ErrInvalidRewrite)
				assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"))

				return
			}

			require.NoError(t, err)
			path, wrote, err := clone.Write(false)
			require.NoError(t, err)
			require.True(t, wrote)
			assert.YAMLEq(t, fmt.Sprintf(cloneRootFieldsTemplate,
				testCase.finalName, "Docker", "kind-local"), readClone(t, root, path))
		})
	}
}

func TestClonePlanContextIsValueExactAndRootOnly(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ name, source, expected, fallback string }{
		{"ordered", "kind-local", "kind-staging", "ignored"},
		{"custom", "custom-context", "custom-context", "ignored"},
		{"partial EKS", "admin@local.eu.eksctl.io", "admin@local.eu.eksctl.io", ""},
		{"empty", "''", "kind-prod", "kind-prod"},
		{"null", "null", "kind-prod", "kind-prod"},
		{"unset without fallback", "null", "null", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := fmt.Sprintf(cloneRootFieldsTemplate, "local", "Docker", testCase.source)
			require.NoError(
				t,
				os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(source), 0o600),
			)

			rewrites := []environment.Rewrite{
				{
					Kind:  environment.MetaFieldValue,
					Field: "context",
					Old:   "kind-local",
					New:   "kind-prod",
				},
				{
					Kind:  environment.MetaFieldValue,
					Field: "context",
					Old:   "kind-prod",
					New:   "kind-staging",
				},
				{
					Kind:  environment.MetaFieldValue,
					Field: "context",
					Old:   "local.eksctl.io",
					New:   "prod.eksctl.io",
				},
			}
			path, _, err := environment.CloneEnvironmentConfigForTarget(root,
				"ksail.local.yaml", "ksail.prod.yaml", rewrites, environment.CloneTarget{
					Identity: environment.Identity{Name: "prod", Context: testCase.fallback},
				}, false)
			require.NoError(t, err)
			assert.YAMLEq(t, fmt.Sprintf(cloneRootFieldsTemplate,
				"prod", "Docker", testCase.expected), readClone(t, root, path))
		})
	}
}

func TestClonePlanRequiresPlaintextNameButPreservesOpaqueEncryptedCopy(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{".yaml", ".enc.yaml"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := "metadata: {name: local}\nspec: {}\n"
			src, dst := "ksail.local"+suffix, "ksail.prod"+suffix
			require.NoError(t, os.WriteFile(filepath.Join(root, src), []byte(source), 0o600))

			path, wrote, err := environment.CloneEnvironmentConfigForTarget(root, src, dst,
				nil, environment.CloneTarget{}, false)
			if suffix == ".yaml" {
				require.ErrorIs(t, err, environment.ErrInvalidConfig)
				assert.False(t, wrote)
				assert.NoFileExists(t, filepath.Join(root, dst))

				return
			}

			require.NoError(t, err)
			assert.True(t, wrote)
			assert.Equal(t, source, readClone(t, root, path))
		})
	}
}

func TestPreparedConfigCloneWritesOnlyItsValidatedSnapshot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	const source = "metadata: {name: local}\nspec: {}\nnote: captured\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(source), 0o600))
	clone, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
		"ksail.local.yaml", "ksail.prod.yaml", nil,
		environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"), "preparation must not write")

	const changedSource = "this source changed after validation\n"
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(changedSource), 0o600),
	)

	path, wrote, err := clone.Write(false)
	require.NoError(t, err)
	assert.True(t, wrote)
	assert.Equal(t, "ksail.prod.yaml", path)
	assert.Contains(t, readClone(t, root, path), "name: prod")
	assert.Contains(t, readClone(t, root, path), "note: captured")
	assert.Equal(t, changedSource, readClone(t, root, "ksail.local.yaml"))
}

func TestPreparedConfigCloneRechecksDestinationOnWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.local.yaml"),
		[]byte("metadata: {name: local}\nspec: {}\n"), 0o600))
	clone, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
		"ksail.local.yaml", "ksail.prod.yaml", nil,
		environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
	require.NoError(t, err)
	outside := filepath.Join(t.TempDir(), "retained.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("retained\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "ksail.prod.yaml")))

	_, wrote, err := clone.Write(true)
	require.ErrorIs(t, err, environment.ErrDestinationEscapesRepository)
	assert.False(t, wrote)

	assert.Equal(t, "retained\n", readClone(t, filepath.Dir(outside), filepath.Base(outside)))

	var empty environment.ConfigClone

	_, wrote, err = empty.Write(true)
	require.ErrorIs(t, err, environment.ErrInvalidConfig)
	assert.False(t, wrote)
}

//nolint:paralleltest // changing the caller's working directory is the regression
func TestPreparedConfigCloneRetainsItsResolvedRepository(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.local.yaml"),
		[]byte("metadata: {name: local}\nspec: {}\n"), 0o600))
	t.Chdir(parent)

	clone, err := environment.PrepareEnvironmentConfigCloneForTarget("workspace",
		"ksail.local.yaml", "ksail.prod.yaml", nil,
		environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
	require.NoError(t, err)
	t.Chdir(t.TempDir())

	_, wrote, err := clone.Write(false)
	require.NoError(t, err)
	assert.True(t, wrote)
	assert.Contains(t, readClone(t, root, "ksail.prod.yaml"), "name: prod")
	assert.NoDirExists(t, "workspace")
}

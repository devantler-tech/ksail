package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestCloneContextRejectsOnlyActualSharedOrInheritedMutations(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, source string
		refused      bool
	}{
		{"inherited matching context", "defaults: &defaults {context: kind-local}\nspec:\n" +
			"  cluster:\n    connection:\n      <<: *defaults\n", true},
		{"aliased context", "original: &context kind-local\nspec:\n" +
			"  cluster:\n    connection:\n      context: *context\n", true},
		{"unchanged shared context", "spec:\n  cluster:\n    connection:\n" +
			"      context: &context custom-context\nsaved: *context\n", false},
		{"unchanged inherited context", "defaults: &defaults {context: custom-context}\nspec:\n" +
			"  cluster:\n    connection:\n      <<: *defaults\n", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(
				t,
				os.WriteFile(
					filepath.Join(root, "ksail.local.yaml"),
					[]byte(testCase.source),
					0o600,
				),
			)

			clone, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
				"ksail.local.yaml", "ksail.prod.yaml", []environment.Rewrite{
					{
						Kind:  environment.MetaFieldValue,
						Field: "context",
						Old:   "kind-local",
						New:   "kind-prod",
					},
				}, environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
			if testCase.refused {
				require.ErrorIs(t, err, environment.ErrInvalidConfig)
				assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"))

				return
			}

			require.NoError(t, err)
			path, _, err := clone.Write(false)
			require.NoError(t, err)
			assert.Contains(t, readClone(t, root, path), "custom-context")
			assert.Equal(t, testCase.source, readClone(t, root, "ksail.local.yaml"))
		})
	}
}

func TestCloneTargetRefusesSharedIdentityAndInheritedParents(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"shared name":     "metadata:\n  name: &cluster local\n  annotations:\n    saved: *cluster\nspec: {}\n",
		"shared metadata": "metadata: &meta\n  name: local\n  annotations: {saved: kept}\nsaved: *meta\n",
		"shared provider": "spec:\n  cluster:\n    provider: &provider Docker\nsaved: *provider\n",
		"merged metadata": "defaults: &defaults\n  metadata:\n    annotations: {saved: kept}\n<<: *defaults\nspec: {}\n",
		"merged cluster":  "defaults: &defaults\n  cluster:\n    distribution: Talos\nspec:\n  <<: *defaults\n",
		"aliased name":    "original: &cluster local\nmetadata:\n  name: *cluster\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(
				t,
				os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(content), 0o600),
			)
			_, wrote, err := environment.CloneEnvironmentConfigForTarget(
				root,
				"ksail.local.yaml",
				"ksail.prod.yaml",
				nil,
				environment.CloneTarget{
					Identity: environment.Identity{Name: "prod", Context: "admin@prod"},
					Provider: "Hetzner",
				},
				false,
			)
			require.ErrorIs(t, err, environment.ErrInvalidConfig)
			require.False(t, wrote)
			require.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"))
			require.Equal(t, content, readClone(t, root, "ksail.local.yaml"))
		})
	}
}

func TestCloneTargetAllowsDirectLeafOverrideAndUnrelatedAliases(t *testing.T) {
	t.Parallel()

	const content = "defaults: &defaults\n  annotations: {saved: kept}\n" +
		"metadata:\n  <<: *defaults\n" +
		"unrelated: &unrelated {value: kept}\ncopy: *unrelated\nspec: {}\n"

	root := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(content), 0o600),
	)
	_, wrote, err := environment.CloneEnvironmentConfigForTarget(
		root,
		"ksail.local.yaml",
		"ksail.prod.yaml",
		nil,
		environment.CloneTarget{
			Identity: environment.Identity{Name: "prod"},
			Provider: "Hetzner",
		},
		false,
	)
	require.NoError(t, err)
	require.True(t, wrote)

	var decoded struct {
		Metadata struct {
			Name        string            `yaml:"name"`
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"metadata"`
		Copy map[string]string `yaml:"copy"`
	}
	require.NoError(t, yamlv3.Unmarshal([]byte(readClone(t, root, "ksail.prod.yaml")), &decoded))
	require.Equal(t, "prod", decoded.Metadata.Name)
	require.Equal(t, "kept", decoded.Metadata.Annotations["saved"])
	require.Equal(t, "kept", decoded.Copy["value"])
}

func TestCloneTargetPreservesAnUnchangedSharedProvider(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"anchor": "metadata: {name: local}\nspec:\n  cluster:\n    provider: &provider Docker\nsaved: *provider\n",
		"alias": "provider: &provider Docker\nmetadata: {name: local}\nspec:\n" +
			"  cluster:\n    provider: *provider\nsaved: *provider\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(
				t,
				os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(content), 0o600),
			)
			_, wrote, err := environment.CloneEnvironmentConfigForTarget(
				root,
				"ksail.local.yaml",
				"ksail.prod.yaml",
				nil,
				environment.CloneTarget{
					Identity: environment.Identity{Name: "prod"},
					Provider: "Docker",
				},
				false,
			)
			require.NoError(t, err)
			require.True(t, wrote)
			cloned := readClone(t, root, "ksail.prod.yaml")
			assert.Contains(t, cloned, "&provider Docker")
			assert.Contains(t, cloned, "*provider")

			var decoded struct {
				Spec struct {
					Cluster struct {
						Provider string `yaml:"provider"`
					} `yaml:"cluster"`
				} `yaml:"spec"`
				Saved string `yaml:"saved"`
			}
			require.NoError(t, yamlv3.Unmarshal([]byte(cloned), &decoded))
			assert.Equal(t, "Docker", decoded.Spec.Cluster.Provider)
			assert.Equal(t, "Docker", decoded.Saved)
			assert.Equal(t, content, readClone(t, root, "ksail.local.yaml"))
		})
	}
}

func TestCloneTargetAllowsContextAbsentFromMergedConnection(t *testing.T) {
	t.Parallel()

	const content = "defaults: &defaults {timeout: 30s}\nmetadata: {name: local}\nspec:\n" +
		"  cluster:\n    connection:\n      <<: *defaults\n"

	root := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(content), 0o600),
	)
	_, wrote, err := environment.CloneEnvironmentConfigForTarget(
		root,
		"ksail.local.yaml",
		"ksail.prod.yaml",
		nil,
		environment.CloneTarget{
			Identity: environment.Identity{Name: "prod", Context: "admin@prod"},
		},
		false,
	)
	require.NoError(t, err)
	require.True(t, wrote)

	var decoded struct {
		Spec struct {
			Cluster struct {
				Connection struct {
					Context string `yaml:"context"`
					Timeout string `yaml:"timeout"`
				} `yaml:"connection"`
			} `yaml:"cluster"`
		} `yaml:"spec"`
	}
	require.NoError(t, yamlv3.Unmarshal([]byte(readClone(t, root, "ksail.prod.yaml")), &decoded))
	assert.Equal(t, "admin@prod", decoded.Spec.Cluster.Connection.Context)
	assert.Equal(t, "30s", decoded.Spec.Cluster.Connection.Timeout)
	assert.Equal(t, content, readClone(t, root, "ksail.local.yaml"))
}

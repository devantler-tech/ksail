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

package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/require"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestCloneTargetRefusesSharedIdentityAndInheritedParents(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"shared name":     "metadata:\n  name: &cluster local\n  annotations:\n    saved: *cluster\nspec: {}\n",
		"shared metadata": "metadata: &meta\n  name: local\n  annotations: {saved: kept}\nsaved: *meta\n",
		"shared provider": "spec:\n  cluster:\n    provider: &provider Docker\nsaved: *provider\n",
		"merged metadata": "defaults: &defaults\n  metadata:\n    annotations: {saved: kept}\n<<: *defaults\nspec: {}\n",
		"merged cluster":  "defaults: &defaults\n  cluster:\n    distribution: Talos\nspec:\n  <<: *defaults\n",
		"aliased name":    "original: &cluster local\nmetadata:\n  name: *cluster\n",
		"merged context": "defaults: &defaults {context: hand-picked}\nspec:\n" +
			"  cluster:\n    connection:\n      <<: *defaults\n",
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

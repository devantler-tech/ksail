package env_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

//nolint:paralleltest // each command resolves its workspace through t.Chdir
func TestAddEnvironmentMaterializesProviderOverride(t *testing.T) {
	for _, baseSynced := range []bool{false, true} {
		for _, provider := range []struct{ name, field string }{
			{name: "omitted"},
			{name: "null", field: "    provider: null\n"},
			{name: "empty", field: "    provider: \"\"\n"},
			{name: "explicit", field: "    provider: Docker\n"},
		} {
			t.Run(fmt.Sprintf("base=%t/%s", baseSynced, provider.name), func(t *testing.T) {
				root, sourceFile, source := writeCloneIdentitySource(
					t,
					baseSynced,
					"local",
					provider.field,
				)
				t.Chdir(root)
				output, err := runAddEnvironment(
					t,
					"prod",
					"--from",
					"local",
					"--provider",
					"Hetzner",
				)
				require.NoError(t, err, output)
				content := readAddEnv(t, root, "ksail.prod.yaml")

				var cloned v1alpha1.Cluster
				require.NoError(t, yaml.Unmarshal([]byte(content), &cloned))
				assert.Equal(
					t,
					v1alpha1.ProviderHetzner,
					cloned.Spec.Cluster.Provider,
					"an explicit override must not fall back to Docker when the source omitted its default",
				)
				assert.Equal(
					t,
					source,
					readAddEnv(t, root, sourceFile),
					"the source must remain unchanged",
				)
				assert.Equal(t, "local", cloned.Annotations["name"])
				assert.Equal(t, "Docker", cloned.Annotations["provider"])
				assert.Contains(t, content, "# preserve local-config and localhost user text")
			})
		}
	}
}

//nolint:paralleltest // each command resolves its workspace through t.Chdir
func TestAddEnvironmentRepointsCustomSourceClusterName(t *testing.T) {
	for _, baseSynced := range []bool{false, true} {
		t.Run(fmt.Sprintf("base=%t", baseSynced), func(t *testing.T) {
			root, sourceFile, source := writeCloneIdentitySource(t, baseSynced,
				"r73-local", "    provider: Docker\n")

			const overlay = "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: cluster-meta}\n" +
				"data:\n  cluster_name: r73-local\n  saved: r73-local\n"
			require.NoError(
				t,
				os.WriteFile(filepath.Join(root, "k8s/clusters/local/cluster-meta.yaml"),
					[]byte(overlay), 0o600),
			)
			t.Chdir(root)
			output, err := runAddEnvironment(t, "prod", "--from", "local")
			require.NoError(t, err, output)
			content := readAddEnv(t, root, "ksail.prod.yaml")

			var cloned v1alpha1.Cluster
			require.NoError(t, yaml.Unmarshal([]byte(content), &cloned))
			assert.Equal(t, "prod", cloned.Name, "the new environment must target its own cluster")
			assert.Equal(t, "r73-local", cloned.Annotations["unrelated-name"],
				"only the root cluster name is repointed")
			assert.Equal(t, "local", cloned.Annotations["name"])
			assert.Equal(
				t,
				source,
				readAddEnv(t, root, sourceFile),
				"the source must remain unchanged",
			)
			assert.Contains(t, content, "# preserve local-config and localhost user text")
			assert.Contains(
				t,
				readAddEnv(t, root, "k8s/clusters/prod/cluster-meta.yaml"),
				"cluster_name: prod",
			)
			assert.Contains(
				t,
				readAddEnv(t, root, "k8s/clusters/prod/cluster-meta.yaml"),
				"saved: r73-local",
			)
			assert.Equal(t, overlay, readAddEnv(t, root, "k8s/clusters/local/cluster-meta.yaml"))
		})
	}
}

//nolint:paralleltest // each command resolves its workspace through t.Chdir
func TestAddEnvironmentRejectsInvalidTargetBeforeWritingOverlay(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%t", force), func(t *testing.T) {
			root, sourceFile, source := writeCloneIdentitySource(
				t,
				false,
				"local",
				"    provider: Docker\n",
			)
			source = strings.Replace(source, "name: local", "name: &shared local", 1)
			source = strings.Replace(source, "unrelated-name: local", "unrelated-name: *shared", 1)
			require.NoError(t, os.WriteFile(filepath.Join(root, sourceFile), []byte(source), 0o600))
			destination := filepath.Join(root, "k8s/clusters/prod/kustomization.yaml")

			args := []string{"prod", "--from", "local"}
			if force {
				args = append(args, "--force")

				require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o750))
				require.NoError(t, os.WriteFile(destination, []byte("existing overlay\n"), 0o600))
			}

			t.Chdir(root)
			output, err := runAddEnvironment(t, args...)
			require.Error(t, err, output)
			assert.Contains(t, output+err.Error(), "shared YAML alias")

			if force {
				assert.Equal(
					t,
					"existing overlay\n",
					readAddEnv(t, root, "k8s/clusters/prod/kustomization.yaml"),
				)
			} else {
				assert.NoDirExists(t, filepath.Dir(destination))
			}

			assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"))
			assert.Equal(t, source, readAddEnv(t, root, sourceFile))
		})
	}
}

//nolint:paralleltest // each command resolves its workspace through t.Chdir
func TestAddEnvironmentPreservesProviderOmissionUnlessExplicit(t *testing.T) {
	for _, baseSynced := range []bool{false, true} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("base=%t/explicit=%t", baseSynced, explicit), func(t *testing.T) {
				root, sourceFile, source := writeCloneIdentitySource(t, baseSynced, "local", "")
				t.Chdir(root)

				args := []string{"prod", "--from", "local"}
				if explicit {
					args = append(args, "--provider", "Docker")
				}

				output, err := runAddEnvironment(t, args...)
				require.NoError(t, err, output)

				var cloned map[string]any
				require.NoError(
					t,
					yaml.Unmarshal([]byte(readAddEnv(t, root, "ksail.prod.yaml")), &cloned),
				)
				spec, ok := cloned["spec"].(map[string]any)
				require.True(t, ok)
				cluster, ok := spec["cluster"].(map[string]any)
				require.True(t, ok)

				provider, present := cluster["provider"]
				assert.Equal(t, explicit, present)

				if explicit {
					assert.Equal(t, "Docker", provider)
				}

				assert.Equal(t, source, readAddEnv(t, root, sourceFile))
			})
		}
	}
}

func writeCloneIdentitySource(
	t *testing.T, baseSynced bool, name, providerField string,
) (string, string, string) {
	t.Helper()
	root := t.TempDir()

	sourceFile := "ksail.local.yaml"
	if baseSynced {
		sourceFile = "ksail.yaml"
	}

	source := fmt.Sprintf(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: %s
  annotations:
    unrelated-name: %s
    name: local
    provider: Docker
spec:
  cluster:
    distribution: Talos
%s  workload:
    sourceDirectory: k8s
    kustomizationFile: clusters/local
# preserve local-config and localhost user text
`, name, name, providerField)

	files := map[string]string{
		sourceFile: source,
		"k8s/clusters/local/kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1beta1\n" +
			"kind: Kustomization\nresources: []\n# preserve local-config\n",
	}
	for file, content := range files {
		path := filepath.Join(root, filepath.FromSlash(file))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	return root, sourceFile, source
}

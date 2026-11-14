package env_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// addEnvSourceConfig is a minimal but realistic ksail.<env>.yaml for the source
// environment: it carries the metadata name, distribution, provider, connection
// context and source directory the clone reads and repoints.
const addEnvSourceConfig = `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: prod
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    connection:
      context: kind-prod
  workload:
    sourceDirectory: k8s
`

// addEnvOverlayKustomization mirrors a real clusters/<env>/kustomization.yaml: a
// cluster-meta patch carrying cluster_name/provider, a clusters/<env> reference,
// and an annotation whose value incidentally contains the env name as a substring.
const addEnvOverlayKustomization = `---
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../base
patches:
  - patch: |
      apiVersion: v1
      kind: ConfigMap
      metadata:
        name: cluster-meta
        annotations:
          config.kubernetes.io/local-config: "true"
      data:
        cluster_name: prod
        provider: Docker
components:
  - clusters/prod/components
`

// writeAddEnvSourceRepo materialises a source "prod" environment (root config +
// overlay) under a fresh temp repo and returns the repo root.
func writeAddEnvSourceRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	files := map[string]string{
		"ksail.prod.yaml":                      addEnvSourceConfig,
		"k8s/clusters/prod/kustomization.yaml": addEnvOverlayKustomization,
	}

	for rel, content := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o750))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}

	return repoRoot
}

// runAddEnvironment executes the add-environment command standalone with args and
// returns its combined output and error.
func runAddEnvironment(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := env.NewAddCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

// readAddEnv reads a file written under the test's repo root.
func readAddEnv(t *testing.T, repoRoot, rel string) string {
	t.Helper()

	//nolint:gosec // G304: reads a file just written under the test's own t.TempDir().
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	require.NoError(t, err)

	return string(data)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_ClonesOverlayAndConfig(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	_, err := runAddEnvironment(t, "staging", "--from", "prod")
	require.NoError(t, err)

	overlay := readAddEnv(t, repoRoot, "k8s/clusters/staging/kustomization.yaml")
	assert.Contains(t, overlay, "cluster_name: staging")
	assert.NotContains(t, overlay, "cluster_name: prod")
	assert.Contains(t, overlay, "clusters/staging/components")
	// The provider was not overridden, so it is untouched.
	assert.Contains(t, overlay, "provider: Docker")
	// An unrelated substring of the env name is left alone.
	assert.Contains(t, overlay, "config.kubernetes.io/local-config")

	config := readAddEnv(t, repoRoot, "ksail.staging.yaml")
	assert.Contains(t, config, "name: staging")
	assert.NotContains(t, config, "name: prod")
	// The context is repointed distribution-awarely (Vanilla -> kind-<env>).
	assert.Contains(t, config, "context: kind-staging")

	var cloned v1alpha1.Cluster

	require.NoError(t, yaml.UnmarshalStrict([]byte(config), &cloned))
	assert.Equal(t, "kind-staging", cloned.Spec.Cluster.Connection.Context)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_ProviderOverride(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	// Kubernetes is a valid provider for the Vanilla distribution.
	_, err := runAddEnvironment(t, "dev", "--from", "prod", "--provider", "Kubernetes")
	require.NoError(t, err)

	overlay := readAddEnv(t, repoRoot, "k8s/clusters/dev/kustomization.yaml")
	assert.Contains(t, overlay, "provider: Kubernetes")
	assert.NotContains(t, overlay, "provider: Docker")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_RejectsSameEnvironment(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	_, err := runAddEnvironment(t, "prod", "--from", "prod")
	require.ErrorIs(t, err, env.ErrSameEnvironment)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_RejectsInvalidName(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	_, err := runAddEnvironment(t, "bad_name", "--from", "prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid environment name")
}

// TestHandleAddEnvironmentRunE_RejectsEmptyNames pins the shared empty-name
// gate on the add verb: ValidateClusterName accepts "" (empty means "use the
// default cluster"), but an empty destination or --from would target the
// malformed ksail..yaml (ksail#6059 review).
//
//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_RejectsEmptyNames(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	_, err := runAddEnvironment(t, "", "--from", "prod")
	require.ErrorIs(t, err, env.ErrEmptyEnvironmentName)

	_, err = runAddEnvironment(t, "staging", "--from", "")
	require.ErrorIs(t, err, env.ErrEmptyEnvironmentName)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_RejectsInvalidSourceName(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	// A path-like source name is rejected before it is interpolated into a file path.
	_, err := runAddEnvironment(t, "staging", "--from", "../prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid source environment name")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_RejectsInvalidProvider(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	// Omni is not a valid provider for the Vanilla distribution.
	_, err := runAddEnvironment(t, "dev", "--from", "prod", "--provider", "Omni")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --provider flag")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_MissingSourceConfig(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)

	_, err := runAddEnvironment(t, "staging", "--from", "ghost")
	require.ErrorIs(t, err, env.ErrSourceConfigLoad)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_MissingSourceOverlay(t *testing.T) {
	repoRoot := t.TempDir()
	// Write only the root config, not the overlay directory.
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "ksail.prod.yaml"), []byte(addEnvSourceConfig), 0o600,
	))
	t.Chdir(repoRoot)

	_, err := runAddEnvironment(t, "staging", "--from", "prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cloning environment overlay")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_SkipsExistingWithoutForce(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	// Pre-create the destination overlay and config so a no-force run skips them.
	require.NoError(t, os.MkdirAll(
		filepath.Join(repoRoot, "k8s", "clusters", "staging"), 0o750,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "k8s", "clusters", "staging", "kustomization.yaml"),
		[]byte("preexisting\n"), 0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "ksail.staging.yaml"), []byte("preexisting\n"), 0o600,
	))
	t.Chdir(repoRoot)

	out, err := runAddEnvironment(t, "staging", "--from", "prod")
	require.NoError(t, err)
	assert.Contains(t, out, "nothing written")

	// The pre-existing files are preserved untouched.
	assert.Equal(t, "preexisting\n",
		readAddEnv(t, repoRoot, "k8s/clusters/staging/kustomization.yaml"))
	assert.Equal(t, "preexisting\n", readAddEnv(t, repoRoot, "ksail.staging.yaml"))
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_ForceOverwritesExisting(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "ksail.staging.yaml"), []byte("stale\n"), 0o600,
	))
	t.Chdir(repoRoot)

	_, err := runAddEnvironment(t, "staging", "--from", "prod", "--force")
	require.NoError(t, err)

	config := readAddEnv(t, repoRoot, "ksail.staging.yaml")
	assert.Contains(t, config, "name: staging")
}

// TestNewAddCmd_Structure pins the command shape of `project env add`: its Use
// line, the from/provider/force flags, and the write permission annotation that
// marks it state-modifying.
func TestNewAddCmd_Structure(t *testing.T) {
	t.Parallel()

	cmd := env.NewAddCmd()

	assert.Equal(t, "add <name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	require.NotNil(t, cmd.Flags().Lookup("from"))
	require.NotNil(t, cmd.Flags().Lookup("provider"))
	require.NotNil(t, cmd.Flags().Lookup("force"))

	// The write annotation marks the command as state-modifying.
	assert.Equal(t, "write", cmd.Annotations[annotations.AnnotationPermission])
}

// addEnvBaseConfig is the workspace base config (ksail.yaml) whose presence marks
// the repository root for the env verbs' upward traversal.
const addEnvBaseConfig = `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: base
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
  workload:
    sourceDirectory: k8s
`

// writeAddEnvWorkspace materialises the source "prod" environment plus the base
// ksail.yaml that marks the workspace root, and returns the repo root.
func writeAddEnvWorkspace(t *testing.T) string {
	t.Helper()

	repoRoot := writeAddEnvSourceRepo(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "ksail.yaml"), []byte(addEnvBaseConfig), 0o600,
	))

	return repoRoot
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_RunsFromSubdirectory(t *testing.T) {
	repoRoot := writeAddEnvWorkspace(t)
	subDir := filepath.Join(repoRoot, "k8s", "clusters", "prod")
	t.Chdir(subDir)

	_, err := runAddEnvironment(t, "staging", "--from", "prod")
	require.NoError(t, err)

	// The workspace root was resolved by upward traversal, so the source config
	// was found and the clone landed at the repo root — not relative to the
	// subdirectory.
	config := readAddEnv(t, repoRoot, "ksail.staging.yaml")
	assert.Contains(t, config, "name: staging")

	overlay := readAddEnv(t, repoRoot, "k8s/clusters/staging/kustomization.yaml")
	assert.Contains(t, overlay, "cluster_name: staging")

	_, statErr := os.Stat(filepath.Join(subDir, "ksail.staging.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	_, statErr = os.Stat(filepath.Join(subDir, "k8s"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleAddEnvironmentRunE_MissingSourceFromSubdirectoryListsAvailable(t *testing.T) {
	repoRoot := writeAddEnvWorkspace(t)
	t.Chdir(filepath.Join(repoRoot, "k8s"))

	_, err := runAddEnvironment(t, "staging", "--from", "ghost")
	require.ErrorIs(t, err, env.ErrSourceConfigLoad)

	// The available-environments hint is discovered at the workspace root, so a
	// mistyped --from still reports what exists from a subdirectory.
	assert.Contains(t, err.Error(), "available environments: prod")
}

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
func TestAddEnvironmentRepointsOnlyDerivedCustomClusterContexts(t *testing.T) {
	for _, baseSynced := range []bool{false, true} {
		for _, testCase := range []struct{ name, distribution, context, expected string }{
			{"metadata Talos", "Talos", "admin@r73-local", "admin@prod"},
			{"label Talos", "Talos", "admin@local", "admin@prod"},
			{"custom Talos", "Talos", "operator-context", "operator-context"},
			{"metadata Vanilla", "Vanilla", "kind-r73-local", "kind-prod"},
			{"label Vanilla", "Vanilla", "kind-local", "kind-prod"},
			{"custom Vanilla", "Vanilla", "operator-context", "operator-context"},
		} {
			t.Run(fmt.Sprintf("base=%t/%s", baseSynced, testCase.name), func(t *testing.T) {
				root, sourceFile, source := writeCloneIdentitySource(t, baseSynced, "r73-local",
					"    provider: Docker\n    connection:\n      context: "+testCase.context+"\n")
				source = strings.Replace(source, "distribution: Talos",
					"distribution: "+testCase.distribution, 1)
				require.NoError(
					t,
					os.WriteFile(filepath.Join(root, sourceFile), []byte(source), 0o600),
				)
				t.Chdir(root)
				output, err := runAddEnvironment(t, "prod", "--from", "local")
				require.NoError(t, err, output)

				var cloned v1alpha1.Cluster

				require.NoError(
					t,
					yaml.UnmarshalStrict([]byte(readAddEnv(t, root, "ksail.prod.yaml")), &cloned),
				)
				assert.Equal(t, "prod", cloned.Name)
				assert.Equal(t, testCase.expected, cloned.Spec.Cluster.Connection.Context)
				assert.Equal(t, source, readAddEnv(t, root, sourceFile))
			})
		}
	}
}

//nolint:paralleltest // each command resolves its workspace through t.Chdir
func TestAddEnvironmentRejectsSourceEKSContextBeforeWrites(t *testing.T) {
	for _, baseSynced := range []bool{false, true} {
		for _, force := range []bool{false, true} {
			for _, context := range []string{
				"iam@local.eu-west-1.eksctl.io",
				"r73-local.eu-west-1.eksctl.io",
				"arn:aws:iam::123456789012:user/operator@r73-local.eu-west-1.eksctl.io",
				"operator@example.test@local.eu-west-1.eksctl.io",
			} {
				t.Run(
					fmt.Sprintf("base=%t/force=%t/%s", baseSynced, force, context),
					func(t *testing.T) {
						root, sourceFile, source := writeCloneIdentitySource(
							t,
							baseSynced,
							"r73-local",
							"    provider: AWS\n    connection:\n      context: "+context+"\n",
						)
						source = strings.Replace(
							source,
							"distribution: Talos",
							"distribution: EKS",
							1,
						)
						require.NoError(
							t,
							os.WriteFile(filepath.Join(root, sourceFile), []byte(source), 0o600),
						)

						args := []string{"prod", "--from", "local"}
						if force {
							args = append(args, "--force")

							writeAddEnvDestination(t, root)
						}

						t.Chdir(root)
						output, err := runAddEnvironment(t, args...)
						require.Error(t, err, output)
						require.ErrorIs(t, err, env.ErrSourceEKSContext)
						assert.Equal(t, source, readAddEnv(t, root, sourceFile))

						if force {
							assert.Equal(
								t,
								"existing config\n",
								readAddEnv(t, root, "ksail.prod.yaml"),
							)
							assert.Equal(t, "existing overlay\n", readAddEnv(t, root,
								"k8s/clusters/prod/kustomization.yaml"))
						} else {
							assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"))
							assert.NoDirExists(t, filepath.Join(root, "k8s/clusters/prod"))
						}
					},
				)
			}
		}
	}
}

func writeAddEnvDestination(t *testing.T, root string) {
	t.Helper()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "ksail.prod.yaml"), []byte("existing config\n"), 0o600),
	)
	overlay := filepath.Join(root, "k8s/clusters/prod/kustomization.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(overlay), 0o750))
	require.NoError(t, os.WriteFile(overlay, []byte("existing overlay\n"), 0o600))
}

//nolint:paralleltest // each command resolves its workspace through t.Chdir
func TestAddEnvironmentPreservesUnmatchedEKSContexts(t *testing.T) {
	for _, baseSynced := range []bool{false, true} {
		for _, context := range []string{
			"operator@other.eu-west-1.eksctl.io", "operator-context",
			"iam@local.eksctl.io", "iam@local..eksctl.io", "",
		} {
			t.Run(fmt.Sprintf("base=%t/%s", baseSynced, context), func(t *testing.T) {
				root, sourceFile, source := writeCloneIdentitySource(t, baseSynced, "r73-local",
					"    provider: AWS\n    connection:\n      context: '"+context+"'\n")
				source = strings.Replace(source, "distribution: Talos", "distribution: EKS", 1)
				require.NoError(
					t,
					os.WriteFile(filepath.Join(root, sourceFile), []byte(source), 0o600),
				)
				t.Chdir(root)
				output, err := runAddEnvironment(t, "prod", "--from", "local")
				require.NoError(t, err, output)

				var cloned v1alpha1.Cluster
				require.NoError(
					t,
					yaml.UnmarshalStrict([]byte(readAddEnv(t, root, "ksail.prod.yaml")), &cloned),
				)
				assert.Equal(t, "prod", cloned.Name)
				assert.Equal(t, context, cloned.Spec.Cluster.Connection.Context)
				assert.Equal(t, source, readAddEnv(t, root, sourceFile))
			})
		}
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

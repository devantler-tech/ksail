package env_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

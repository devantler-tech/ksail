package environment_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errStubLoad is returned by stubLoader for a file it has no config for, exercising
// DeriveEnvironments' skip-on-load-error path.
var errStubLoad = errors.New("stub load failed")

// stubLoader builds a ConfigLoader from a map of file name to the config it should
// return; a file name mapped to nil (or absent) yields errStubLoad, exercising the
// skip-on-error path.
func stubLoader(configs map[string]*v1alpha1.Cluster) environment.ConfigLoader {
	return func(configFile string) (*v1alpha1.Cluster, error) {
		cfg, ok := configs[configFile]
		if !ok || cfg == nil {
			return nil, errStubLoad
		}

		return cfg, nil
	}
}

func clusterConfig(dist v1alpha1.Distribution, prov v1alpha1.Provider) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Distribution = dist
	cfg.Spec.Cluster.Provider = prov

	return cfg
}

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()

	for _, name := range names {
		filePath := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(filePath, []byte("apiVersion: ksail.io/v1alpha1\n"), 0o600))
	}
}

func TestDeriveEnvironmentsEnumeratesDeclaredEnvironments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.yaml", "ksail.prod.yaml", "ksail.local.yaml", "README.md")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "clusters"), 0o750))

	loader := stubLoader(map[string]*v1alpha1.Cluster{
		"ksail.prod.yaml":  clusterConfig(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner),
		"ksail.local.yaml": clusterConfig(v1alpha1.DistributionTalos, v1alpha1.ProviderDocker),
	})

	envs, err := environment.DeriveEnvironments(dir, loader)
	require.NoError(t, err)
	require.Len(t, envs, 2)

	// Sorted by name: local before prod.
	assert.Equal(t, "local", envs[0].Name)
	assert.Equal(t, "ksail.local.yaml", envs[0].ConfigFile)
	assert.Equal(t, v1alpha1.ProviderDocker, envs[0].Provider)
	assert.Equal(t, "prod", envs[1].Name)
	assert.Equal(t, v1alpha1.ProviderHetzner, envs[1].Provider)
	assert.Equal(t, v1alpha1.DistributionTalos, envs[1].Distribution)
}

func TestDeriveEnvironmentsExcludesBaseAndNonEnvironmentFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// ksail.yaml is the base (no name); ksail.prod.backup.yaml has a non-label name;
	// notksail.dev.yaml lacks the prefix; ksail.dev.yml has the wrong suffix.
	writeFiles(t, dir, "ksail.yaml", "ksail.prod.backup.yaml", "notksail.dev.yaml", "ksail.dev.yml")

	// Every file is excluded by name before the loader runs, so a nil-config loader
	// (which errors for any file) proves none of them is read as an environment.
	envs, err := environment.DeriveEnvironments(dir, stubLoader(nil))
	require.NoError(t, err)
	assert.Empty(t, envs)
}

func TestDeriveEnvironmentsSkipsUnloadableConfigs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.prod.yaml", "ksail.broken.yaml")

	// Only prod loads; broken returns an error and must be skipped, not surfaced.
	loader := stubLoader(map[string]*v1alpha1.Cluster{
		"ksail.prod.yaml": clusterConfig(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner),
	})

	envs, err := environment.DeriveEnvironments(dir, loader)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	assert.Equal(t, "prod", envs[0].Name)
}

func TestDeriveEnvironmentsErrorsOnUnreadableRoot(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist")

	envs, err := environment.DeriveEnvironments(missing, stubLoader(nil))
	require.ErrorIs(t, err, environment.ErrDiscoverEnvironments)
	assert.Nil(t, envs)
}

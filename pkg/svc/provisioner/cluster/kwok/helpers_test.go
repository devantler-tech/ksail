package kwokprovisioner_test

import (
	"os"
	"path/filepath"
	"testing"

	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kwok/pkg/config"
)

func TestTransientCreateErrors(t *testing.T) {
	t.Parallel()

	errs := kwokprovisioner.TransientCreateErrorsForTest()

	assert.Contains(t, errs, "toomanyrequests")
	assert.Contains(t, errs, "Quota exceeded")
	assert.Contains(t, errs, "i/o timeout")
	assert.Contains(t, errs, "no such host")
}

func TestKwokContainerNames(t *testing.T) {
	t.Parallel()

	names := kwokprovisioner.KwokContainerNamesForTest("demo")

	assert.Equal(t, []string{
		"kwok-demo-kube-apiserver",
		"kwok-demo-kube-controller-manager",
		"kwok-demo-kube-scheduler",
		"kwok-demo-kwok-controller",
	}, names)
}

func TestKwokStateDir(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	dir, err := kwokprovisioner.KwokStateDirForTest("demo")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, ".kwok", "clusters", "demo"), dir)
}

func TestResolveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configured  string
		argument    string
		wantResolve string
	}{
		{
			name:        "explicit argument wins",
			configured:  "configured",
			argument:    "explicit",
			wantResolve: "explicit",
		},
		{
			name:        "falls back to configured name",
			configured:  "configured",
			argument:    "",
			wantResolve: "configured",
		},
		{name: "empty when nothing set", configured: "", argument: "", wantResolve: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := kwokprovisioner.NewProvisioner(testCase.configured, "", nil)
			assert.Equal(t, testCase.wantResolve, prov.ResolveNameForTest(testCase.argument))
		})
	}
}

// TestSetDefaultCluster mutates the process-global config.DefaultCluster, so it
// must not run in parallel with other tests touching that global.
//
//nolint:paralleltest // Mutates process-global config.DefaultCluster.
func TestSetDefaultCluster(t *testing.T) {
	original := config.DefaultCluster

	t.Cleanup(func() { config.DefaultCluster = original })

	restore := kwokprovisioner.SetDefaultClusterForTest("temp-cluster")

	assert.Equal(t, "temp-cluster", config.DefaultCluster)

	restore()

	assert.Equal(t, original, config.DefaultCluster)
}

func TestResolveConfigPath_ExplicitPath(t *testing.T) {
	t.Parallel()

	prov := kwokprovisioner.NewProvisioner("demo", "/explicit/kwok.yaml", nil)

	path, cleanup, err := prov.ResolveConfigPathForTest()

	require.NoError(t, err)
	assert.Equal(t, "/explicit/kwok.yaml", path)
	assert.Nil(t, cleanup, "explicit config path needs no cleanup")
}

func TestResolveConfigPath_GeneratesDefault(t *testing.T) {
	t.Parallel()

	prov := kwokprovisioner.NewProvisioner("demo", "", nil)

	path, cleanup, err := prov.ResolveConfigPathForTest()
	require.NoError(t, err)
	require.NotNil(t, cleanup)

	defer cleanup()

	// A temp directory containing kustomization.yaml and simulation.yaml is created.
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	kustomizationPath := filepath.Join(path, "kustomization.yaml")
	kustomization, err := os.ReadFile(kustomizationPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(kustomization), "kind: Kustomization")
	assert.Contains(t, string(kustomization), "simulation.yaml")

	simulationPath := filepath.Join(path, "simulation.yaml")
	simulation, err := os.ReadFile(simulationPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.NotEmpty(t, simulation)

	// cleanup removes the generated directory.
	cleanup()

	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

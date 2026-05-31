package kwokprovisioner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newKubernetesProvisionerForTest builds a KubernetesProvisioner with only the
// fields the pure helpers under test rely on (name + kubeconfig path). The
// Kubernetes infrastructure provider is left nil because none of the tested
// helpers exercise it.
func newKubernetesProvisionerForTest(
	t *testing.T,
	name, kubeconfigPath string,
) *kwokprovisioner.KubernetesProvisioner {
	t.Helper()

	prov, err := kwokprovisioner.NewKubernetesProvisioner(
		kwokprovisioner.KubernetesProvisionerConfig{
			Name:           name,
			KubeconfigPath: kubeconfigPath,
		},
	)
	require.NoError(t, err)

	return prov
}

// writeKubeconfig writes a minimal kubeconfig with a single cluster entry and
// returns its path.
func writeKubeconfig(t *testing.T, clusterName, server string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	content := "apiVersion: v1\n" +
		"kind: Config\n" +
		"clusters:\n" +
		"- name: " + clusterName + "\n" +
		"  cluster:\n" +
		"    server: " + server + "\n"

	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestDiscoverAPIServerPort_Success(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeKubeconfig(t, "kwok-demo", "https://127.0.0.1:38423")
	prov := newKubernetesProvisionerForTest(t, "demo", kubeconfigPath)

	port, err := prov.DiscoverAPIServerPortForTest("")
	require.NoError(t, err)
	assert.Equal(t, 38423, port)
}

func TestDiscoverAPIServerPort_ExplicitNameWins(t *testing.T) {
	t.Parallel()

	// The kubeconfig only holds an entry for the explicit cluster name, proving
	// the explicit argument (not the configured name) drives the lookup.
	kubeconfigPath := writeKubeconfig(t, "kwok-other", "https://127.0.0.1:12345")
	prov := newKubernetesProvisionerForTest(t, "demo", kubeconfigPath)

	port, err := prov.DiscoverAPIServerPortForTest("other")
	require.NoError(t, err)
	assert.Equal(t, 12345, port)
}

func TestDiscoverAPIServerPort_LoadError(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	prov := newKubernetesProvisionerForTest(t, "demo", missing)

	_, err := prov.DiscoverAPIServerPortForTest("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load kubeconfig")
}

func TestDiscoverAPIServerPort_ClusterEntryNotFound(t *testing.T) {
	t.Parallel()

	// Entry is keyed "kwok-present" but we resolve "kwok-absent".
	kubeconfigPath := writeKubeconfig(t, "kwok-present", "https://127.0.0.1:6443")
	prov := newKubernetesProvisionerForTest(t, "absent", kubeconfigPath)

	_, err := prov.DiscoverAPIServerPortForTest("")
	require.ErrorIs(t, err, k8s.ErrClusterEntryNotFound)
}

func TestDiscoverAPIServerPort_UnparseableServer(t *testing.T) {
	t.Parallel()

	// A server URL that does not match the https://127.0.0.1:<port> shape.
	kubeconfigPath := writeKubeconfig(t, "kwok-demo", "https://example.com")
	prov := newKubernetesProvisionerForTest(t, "demo", kubeconfigPath)

	_, err := prov.DiscoverAPIServerPortForTest("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse API server port")
}

func TestApplyKwokCertSANs(t *testing.T) {
	t.Parallel()

	prov := newKubernetesProvisionerForTest(t, "demo", "")
	originalConfigPath := prov.ConfigPathForTest()

	cleanup, err := prov.ApplyKwokCertSANsForTest("203.0.113.7")
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	// Guard against an early assertion failure leaving the temp config dir
	// behind / configPath unrestored. cleanup is idempotent (RemoveAll on a
	// gone dir is a no-op, configPath is re-set to the same value), so the
	// explicit cleanup() call below remains safe.
	t.Cleanup(cleanup)

	dir := prov.ConfigPathForTest()
	assert.NotEqual(t, originalConfigPath, dir, "configPath should point at the temp config dir")

	for _, name := range []string{"kustomization.yaml", "simulation.yaml", "kwokctl.yaml"} {
		_, statErr := os.Stat(filepath.Join(dir, name))
		require.NoError(t, statErr, "expected %s to be written", name)
	}

	kwokctlPath := filepath.Join(dir, "kwokctl.yaml")
	kwokctl, err := os.ReadFile(kwokctlPath) //nolint:gosec // test path under TempDir
	require.NoError(t, err)
	assert.Contains(
		t,
		string(kwokctl),
		"203.0.113.7",
		"the exposure address must be added to cert SANs",
	)
	assert.Contains(
		t,
		string(kwokctl),
		"kubeApiserverPort: 6443",
		"the API server port must be pinned",
	)

	kustomizationPath := filepath.Join(dir, "kustomization.yaml")
	kustomization, err := os.ReadFile(kustomizationPath) //nolint:gosec // test path under TempDir
	require.NoError(t, err)
	assert.Contains(t, string(kustomization), "kind: Kustomization")
	assert.Contains(t, string(kustomization), "simulation.yaml")
	assert.Contains(t, string(kustomization), "kwokctl.yaml")

	// cleanup restores the original config path and removes the temp dir.
	cleanup()
	assert.Equal(t, originalConfigPath, prov.ConfigPathForTest())

	_, statErr := os.Stat(dir)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

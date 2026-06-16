package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	cluster "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver is a credentials.Resolver driven entirely by in-memory values, so
// the cloud-provider probe can be exercised without mutating the process
// environment. It mirrors the secure-store override path the UI uses.
type fakeResolver struct {
	value  string
	envVar string
}

func (f fakeResolver) Value(credentials.Key) string  { return f.value }
func (f fakeResolver) EnvVar(credentials.Key) string { return f.envVar }

// talosPublicIPKubeconfig is a kubeconfig for a Talos cluster reachable via a
// public IP, which forces detection down the cloud-provider probe path.
const talosPublicIPKubeconfig = `apiVersion: v1
kind: Config
current-context: admin@prod-cluster
clusters:
- cluster:
    server: https://1.2.3.4:6443
  name: prod-cluster
contexts:
- context:
    cluster: prod-cluster
    user: admin@prod-cluster
  name: admin@prod-cluster
users:
- name: admin@prod-cluster
  user:
    client-certificate-data: ""
`

func writeTalosKubeconfig(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	err := os.WriteFile(kubeconfigPath, []byte(talosPublicIPKubeconfig), 0o600)
	require.NoError(t, err)

	return kubeconfigPath
}

// TestDetectInfoWithResolver_NoTokenUsesResolverEnvVarName verifies that the
// "no credentials" error names the env var the injected resolver reports, not a
// hardcoded HCLOUD_TOKEN — proving the resolver drives the message.
func TestDetectInfoWithResolver_NoTokenUsesResolverEnvVarName(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeTalosKubeconfig(t)

	resolver := fakeResolver{value: "", envVar: "CUSTOM_HETZNER_TOKEN"}

	_, err := cluster.DetectInfoWithResolver(t.Context(), kubeconfigPath, "", resolver)

	require.Error(t, err)
	require.ErrorIs(t, err, cluster.ErrNoCloudCredentials)
	assert.Contains(t, err.Error(), "CUSTOM_HETZNER_TOKEN")
}

// TestDetectInfoWithResolver_TokenSetButIPNotFound verifies that when the
// resolver supplies a token (no env mutation), the probe runs and surfaces
// ErrUnableToDetectProvider for an IP no provider owns.
func TestDetectInfoWithResolver_TokenSetButIPNotFound(t *testing.T) {
	t.Parallel()

	kubeconfigPath := writeTalosKubeconfig(t)

	resolver := fakeResolver{value: "fake-token-that-will-fail", envVar: "HCLOUD_TOKEN"}

	_, err := cluster.DetectInfoWithResolver(t.Context(), kubeconfigPath, "", resolver)

	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrUnableToDetectProvider)
}

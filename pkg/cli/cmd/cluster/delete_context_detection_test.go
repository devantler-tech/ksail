package cluster_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	cluster "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDeleteDetectionKubeconfig writes a kubeconfig whose single context is
// named contextName and whose server is a localhost endpoint (so provider
// detection resolves to Docker).
func writeDeleteDetectionKubeconfig(t *testing.T, contextName string) string {
	t.Helper()

	content := `apiVersion: v1
kind: Config
current-context: ` + contextName + `
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: ` + contextName + `
contexts:
- context:
    cluster: ` + contextName + `
    user: ` + contextName + `
  name: ` + contextName + `
users:
- name: ` + contextName + `
  user: {}
`
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// TestDetectClusterDistribution_RecognizesTalos proves the deliberate
// improvement in plan item 2.1: cluster delete now probes the Talos ("admin@")
// context prefix — previously its hardcoded prefix list only covered
// kind-/k3d-/vcluster-docker_/kwok-, so a Talos-on-Docker cluster's
// distribution went undetected. The prefixes now come from the shared
// clusterdetector.ContextPrefixes single source.
//
// Note: the Docker-provider delete path is exercised here. The nested-K3s
// "k3k-" alias is included in ContextPrefixes for the inverse-mapping call
// sites (switch/lifecycle), but k3k clusters live on the Kubernetes provider,
// not Docker, so DetectInfo (which classifies Docker-localhost endpoints) does
// not resolve a k3k context to a distribution on this path — verified below.
func TestDetectClusterDistribution_RecognizesTalos(t *testing.T) {
	t.Parallel()

	t.Run("talos admin@ context is detected", func(t *testing.T) {
		t.Parallel()

		kubeconfigPath := writeDeleteDetectionKubeconfig(t, "admin@my-talos")

		info := cluster.ExportDetectClusterDistribution("my-talos", kubeconfigPath)

		require.NotNil(t, info, "delete should detect a Talos admin@ context")
		assert.Equal(t, v1alpha1.DistributionTalos, info.Distribution)
	})

	t.Run("vanilla kind- context still detected", func(t *testing.T) {
		t.Parallel()

		kubeconfigPath := writeDeleteDetectionKubeconfig(t, "kind-app")

		info := cluster.ExportDetectClusterDistribution("app", kubeconfigPath)

		require.NotNil(t, info)
		assert.Equal(t, v1alpha1.DistributionVanilla, info.Distribution)
	})

	t.Run("k3k- context not classified on the docker path", func(t *testing.T) {
		t.Parallel()

		kubeconfigPath := writeDeleteDetectionKubeconfig(t, "k3k-nested")

		// k3k clusters live on the Kubernetes provider; DetectInfo cannot
		// classify the context as a Docker distribution, so detection yields nil
		// and delete proceeds via its other (container-name) fallbacks.
		info := cluster.ExportDetectClusterDistribution("nested", kubeconfigPath)

		assert.Nil(t, info)
	})
}

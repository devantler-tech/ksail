package talos_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	taloskubeconfig "github.com/siderolabs/talos/pkg/kubeconfig"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

// TestConfigsWithNameKeepsNativeKubeconfigsSeparate reproduces a nested cluster
// inheriting its host's name patch, including later endpoint and SAN rewrites.
func TestConfigsWithNameKeepsNativeKubeconfigsSeparate(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		scopes []talos.PatchScope
	}{
		{name: "cluster patch", scopes: []talos.PatchScope{talos.PatchScopeCluster}},
		{name: "role patches", scopes: []talos.PatchScope{
			talos.PatchScopeControlPlane, talos.PatchScopeWorker,
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			host := inheritedHostConfig(t, testCase.scopes)
			hostRaw := nativeAdminKubeconfig(t, host)

			nested, err := host.WithName("nested-talos")
			require.NoError(t, err)
			assert.Contains(
				t,
				nested.ControlPlane().Machine().Security().CertSANs(),
				"keep.example.test",
			)
			assert.Contains(t, nested.Worker().Machine().Security().CertSANs(), "keep.example.test")
			nested, err = nested.WithEndpoint("127.0.0.1")
			require.NoError(t, err)
			nested, err = nested.WithCertSANs([]string{"keep.example.test", "nested.example.test"})
			require.NoError(t, err)
			assert.Equal(t, "nested-talos", nested.Worker().Cluster().Name())

			nestedRaw := nativeAdminKubeconfig(t, nested)
			nestedConfig, err := clientcmd.Load(nestedRaw)
			require.NoError(t, err)
			require.Contains(t, nestedConfig.Clusters, "nested-talos")
			assert.NotContains(t, nestedConfig.Clusters, "host-fixture")
			assert.Equal(t, "admin@nested-talos", nestedConfig.CurrentContext)

			requireSeparateKubeconfigEntries(t, hostRaw, nestedRaw)
			assert.Equal(t, "host-fixture", host.ControlPlane().Cluster().Name())
			assert.Contains(
				t,
				host.ControlPlane().Machine().Security().CertSANs(),
				"keep.example.test",
			)
		})
	}
}

// requireSeparateKubeconfigEntries exercises the production merge and repoint
// steps, checking that the host survives the nested bootstrap-address rewrite.
func requireSeparateKubeconfigEntries(t *testing.T, hostRaw, nestedRaw []byte) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, k8s.MergeKubeconfig(path, hostRaw))
	hostConfig, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	require.NoError(t, k8s.MergeKubeconfig(path, nestedRaw))
	require.NoError(t, k8s.ModifyKubeconfigCluster(
		path, "nested-talos", "https://nested.example.test:6443",
	))
	merged, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, hostConfig.Clusters["host-fixture"], merged.Clusters["host-fixture"])
	assert.Equal(t, "https://nested.example.test:6443", merged.Clusters["nested-talos"].Server)
}

// inheritedHostConfig models scaffolded host settings applied at either cluster
// scope or node-role scope, including an unrelated setting that must survive.
func inheritedHostConfig(t *testing.T, scopes []talos.PatchScope) *talos.Configs {
	t.Helper()

	patches := make([]talos.Patch, 0, len(scopes))
	for _, scope := range scopes {
		patches = append(patches, talos.Patch{
			Path: "inherited-host-config", Scope: scope,
			Content: []byte("cluster:\n  clusterName: host-fixture\n" +
				"machine:\n  certSANs: [keep.example.test]\n"),
		})
	}

	base, err := talos.NewDefaultConfigsWithVersionContractAndPatches(
		talos.DefaultKubernetesVersion, talosconfig.TalosVersion1_14, patches,
	)
	require.NoError(t, err)
	host, err := base.WithName("host-fixture")
	require.NoError(t, err)

	return host
}

// TestConfigsWithNameRepairsAnInheritedNameEvenWhenMetadataMatches prevents the
// unchanged-name shortcut from retaining a conflicting machine-config patch.
func TestConfigsWithNameRepairsAnInheritedNameEvenWhenMetadataMatches(t *testing.T) {
	t.Parallel()

	base, err := talos.NewDefaultConfigsWithVersionContractAndPatches(
		talos.DefaultKubernetesVersion, talosconfig.TalosVersion1_14,
		[]talos.Patch{{
			Path: "inherited-name", Scope: talos.PatchScopeCluster,
			Content: []byte("cluster:\n  clusterName: inherited-host\n"),
		}},
	)
	require.NoError(t, err)
	repaired, err := base.WithName(base.Name)
	require.NoError(t, err)
	assert.Equal(t, base.Name, repaired.ControlPlane().Cluster().Name())
	assert.Equal(t, base.Name, repaired.Worker().Cluster().Name())
}

// nativeAdminKubeconfig uses Talos's actual admin generator rather than assuming
// the cluster-entry name a control plane will publish.
func nativeAdminKubeconfig(t *testing.T, configs *talos.Configs) []byte {
	t.Helper()

	var raw bytes.Buffer
	require.NoError(t, taloskubeconfig.GenerateAdmin(configs.ControlPlane().Cluster(), &raw))

	return raw.Bytes()
}

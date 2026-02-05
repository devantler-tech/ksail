package kind_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	kind "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	"github.com/stretchr/testify/assert"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestResolveClusterName_NilConfigs(t *testing.T) {
	t.Parallel()

	name := kind.ResolveClusterName(nil, nil)
	assert.Equal(t, "kind", name)
}

func TestResolveClusterName_KindConfigName(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: "my-kind-cluster"}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "ignored-context"

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "my-kind-cluster", name)
}

func TestResolveClusterName_FallbackToContext(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "my-context"

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "my-context", name)
}

func TestResolveClusterName_NilKindConfig(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "my-context"

	name := kind.ResolveClusterName(clusterCfg, nil)
	assert.Equal(t, "my-context", name)
}

func TestResolveClusterName_EmptyNames(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: ""}
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = ""

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "kind", name)
}

func TestResolveClusterName_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: "  my-cluster  "}

	name := kind.ResolveClusterName(nil, kindConfig)
	assert.Equal(t, "my-cluster", name)
}

func TestResolveClusterName_WhitespaceOnlyName(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{Name: "   "}
	clusterCfg := &v1alpha1.Cluster{}

	name := kind.ResolveClusterName(clusterCfg, kindConfig)
	assert.Equal(t, "kind", name)
}

func TestResolveMirrorsDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		clusterCfg *v1alpha1.Cluster
		expected   string
	}{
		{
			name:       "returns_default_when_nil_config",
			clusterCfg: nil,
			expected:   kind.DefaultMirrorsDir,
		},
		{
			name:       "returns_default_when_mirrors_dir_empty",
			clusterCfg: &v1alpha1.Cluster{},
			expected:   kind.DefaultMirrorsDir,
		},
		{
			name: "returns_configured_mirrors_dir",
			clusterCfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Vanilla: v1alpha1.OptionsVanilla{
							MirrorsDir: "custom/mirrors",
						},
					},
				},
			},
			expected: "custom/mirrors",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := kind.ResolveMirrorsDir(testCase.clusterCfg)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestApplyKubeletCertRotationPatches(t *testing.T) {
	t.Parallel()

	t.Run("creates_default_node_when_no_nodes_exist", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{
			Nodes: []kindv1alpha4.Node{},
		}

		kind.ApplyKubeletCertRotationPatches(kindConfig)

		assert.Len(t, kindConfig.Nodes, 1)
		assert.Equal(t, kindv1alpha4.ControlPlaneRole, kindConfig.Nodes[0].Role)
		assert.Len(t, kindConfig.Nodes[0].KubeadmConfigPatches, 1)
		assert.Contains(t, kindConfig.Nodes[0].KubeadmConfigPatches[0], "serverTLSBootstrap: true")
	})

	t.Run("adds_patch_to_single_existing_node", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{
			Nodes: []kindv1alpha4.Node{
				{Role: kindv1alpha4.ControlPlaneRole},
			},
		}

		kind.ApplyKubeletCertRotationPatches(kindConfig)

		assert.Len(t, kindConfig.Nodes, 1)
		assert.Len(t, kindConfig.Nodes[0].KubeadmConfigPatches, 1)
		assert.Equal(t, kind.KubeletCertRotationPatch, kindConfig.Nodes[0].KubeadmConfigPatches[0])
	})

	t.Run("adds_patch_to_all_nodes", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{
			Nodes: []kindv1alpha4.Node{
				{Role: kindv1alpha4.ControlPlaneRole},
				{Role: kindv1alpha4.WorkerRole},
				{Role: kindv1alpha4.WorkerRole},
			},
		}

		kind.ApplyKubeletCertRotationPatches(kindConfig)

		assert.Len(t, kindConfig.Nodes, 3)
		for i, node := range kindConfig.Nodes {
			assert.Len(t, node.KubeadmConfigPatches, 1, "node %d should have 1 patch", i)
			assert.Equal(t, kind.KubeletCertRotationPatch, node.KubeadmConfigPatches[0])
		}
	})

	t.Run("appends_to_existing_patches", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{
			Nodes: []kindv1alpha4.Node{
				{
					Role: kindv1alpha4.ControlPlaneRole,
					KubeadmConfigPatches: []string{
						"existing-patch",
					},
				},
			},
		}

		kind.ApplyKubeletCertRotationPatches(kindConfig)

		assert.Len(t, kindConfig.Nodes[0].KubeadmConfigPatches, 2)
		assert.Equal(t, "existing-patch", kindConfig.Nodes[0].KubeadmConfigPatches[0])
		assert.Equal(t, kind.KubeletCertRotationPatch, kindConfig.Nodes[0].KubeadmConfigPatches[1])
	})
}

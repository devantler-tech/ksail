package kind_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	kind "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
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

func TestResolveClusterName_StripsKindPrefix(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "kind-mycluster"

	name := kind.ResolveClusterName(clusterCfg, nil)
	assert.Equal(t, "mycluster", name)
}

func TestResolveClusterName_RoundTripsWithContextName(t *testing.T) {
	t.Parallel()

	// Simulate the round-trip: ContextName("kind") -> "kind-kind" -> ResolveClusterName -> "kind"
	dist := v1alpha1.DistributionVanilla
	originalName := "kind"
	contextName := dist.ContextName(originalName)

	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = contextName

	resolved := kind.ResolveClusterName(clusterCfg, nil)
	assert.Equal(t, originalName, resolved, "ResolveClusterName should reverse ContextName")
}

func TestResolveClusterName_KindOnlyPrefix(t *testing.T) {
	t.Parallel()

	// Context is exactly "kind-" with nothing after it — should return raw context
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Spec.Cluster.Connection.Context = "kind-"

	name := kind.ResolveClusterName(clusterCfg, nil)
	assert.Equal(t, "kind-", name)
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

//nolint:funlen // Table-driven test covering multiple scenarios.
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

func TestApplyImageVerificationPatches(t *testing.T) {
	t.Parallel()

	t.Run("adds_containerd_config_patch_to_empty_cluster", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{}

		kind.ApplyImageVerificationPatches(kindConfig)

		assert.Len(t, kindConfig.ContainerdConfigPatches, 1)
		assert.Contains(
			t,
			kindConfig.ContainerdConfigPatches[0],
			`io.containerd.image-verifier.v1.bindir`,
		)
		assert.Contains(t, kindConfig.ContainerdConfigPatches[0], `bin_dir`)
		assert.Contains(t, kindConfig.ContainerdConfigPatches[0], `max_verifiers`)
		assert.Contains(t, kindConfig.ContainerdConfigPatches[0], `per_verifier_timeout`)
	})

	t.Run("appends_to_existing_containerd_patches", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{
			ContainerdConfigPatches: []string{"existing-patch"},
		}

		kind.ApplyImageVerificationPatches(kindConfig)

		assert.Len(t, kindConfig.ContainerdConfigPatches, 2)
		assert.Equal(t, "existing-patch", kindConfig.ContainerdConfigPatches[0])
		assert.Equal(t, kind.ImageVerificationPatch, kindConfig.ContainerdConfigPatches[1])
	})

	t.Run("patch_equals_exported_constant", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{}

		kind.ApplyImageVerificationPatches(kindConfig)

		assert.Equal(t, kind.ImageVerificationPatch, kindConfig.ContainerdConfigPatches[0])
	})

	t.Run("idempotent_does_not_duplicate_patch", func(t *testing.T) {
		t.Parallel()

		kindConfig := &kindv1alpha4.Cluster{}

		kind.ApplyImageVerificationPatches(kindConfig)
		kind.ApplyImageVerificationPatches(kindConfig)

		assert.Len(t, kindConfig.ContainerdConfigPatches, 1,
			"calling ApplyImageVerificationPatches twice should not duplicate the patch")
	})
}

// oidcExtraArgs unmarshals a generated kubeadm patch and returns its
// apiServer.extraArgs as a name->value map. Parsing into a list of {name,value}
// entries fails unless the patch uses the v1beta4 list form, so a successful
// parse doubles as an assertion that the map form was not emitted.
func oidcExtraArgs(t *testing.T, patch string) map[string]string {
	t.Helper()

	var parsed struct {
		APIServer struct {
			ExtraArgs []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"extraArgs"`
		} `json:"apiServer"`
	}

	require.NoError(t, yaml.Unmarshal([]byte(patch), &parsed))

	got := make(map[string]string, len(parsed.APIServer.ExtraArgs))
	for _, a := range parsed.APIServer.ExtraArgs {
		got[a.Name] = a.Value
	}

	return got
}

func TestApplyOIDCPatches_NilAddsNoPatch(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{
		Nodes: []kindv1alpha4.Node{{Role: kindv1alpha4.ControlPlaneRole}},
	}

	require.NoError(t, kind.ApplyOIDCPatches(kindConfig, nil))
	assert.Empty(t, kindConfig.Nodes[0].KubeadmConfigPatches)
}

func TestApplyOIDCPatches_DisabledAddsNoPatch(t *testing.T) {
	t.Parallel()

	kindConfig := &kindv1alpha4.Cluster{
		Nodes: []kindv1alpha4.Node{{Role: kindv1alpha4.ControlPlaneRole}},
	}

	require.NoError(t, kind.ApplyOIDCPatches(kindConfig, &v1alpha1.OIDCSpec{}))
	assert.Empty(t, kindConfig.Nodes[0].KubeadmConfigPatches)
}

func TestApplyOIDCPatches_EmitsV1beta4ListForm(t *testing.T) {
	t.Parallel()

	oidc := &v1alpha1.OIDCSpec{
		IssuerURL:      "https://dex.example.com",
		ClientID:       "ksail",
		UsernameClaim:  "email",
		UsernamePrefix: "oidc:",
		GroupsClaim:    "groups",
		GroupsPrefix:   "oidc:",
	}

	kindConfig := &kindv1alpha4.Cluster{
		Nodes: []kindv1alpha4.Node{{Role: kindv1alpha4.ControlPlaneRole}},
	}

	require.NoError(t, kind.ApplyOIDCPatches(kindConfig, oidc))
	require.Len(t, kindConfig.Nodes[0].KubeadmConfigPatches, 1)

	patch := kindConfig.Nodes[0].KubeadmConfigPatches[0]

	// kubeadm v1beta4 silently ignores the map form ("    oidc-issuer-url: ..."),
	// so guard against regressing to it.
	assert.NotContains(t, patch, "\n    oidc-issuer-url:")
	assert.Contains(t, patch, "apiVersion: kubeadm.k8s.io/v1beta4")

	assert.Equal(t, map[string]string{
		"oidc-issuer-url":      "https://dex.example.com",
		"oidc-client-id":       "ksail",
		"oidc-username-claim":  "email",
		"oidc-username-prefix": "oidc:",
		"oidc-groups-claim":    "groups",
		"oidc-groups-prefix":   "oidc:",
	}, oidcExtraArgs(t, patch))
}

func TestApplyOIDCPatches_CAFileAddsArgAndMounts(t *testing.T) {
	t.Parallel()

	caFile := filepath.Join(t.TempDir(), "oidc-ca.crt")
	require.NoError(t, os.WriteFile(caFile, []byte("test-ca"), 0o600))

	oidc := &v1alpha1.OIDCSpec{
		IssuerURL: "https://dex.example.com",
		ClientID:  "ksail",
		CAFile:    caFile,
	}

	kindConfig := &kindv1alpha4.Cluster{
		Nodes: []kindv1alpha4.Node{{Role: kindv1alpha4.ControlPlaneRole}},
	}

	require.NoError(t, kind.ApplyOIDCPatches(kindConfig, oidc))
	require.Len(t, kindConfig.Nodes[0].KubeadmConfigPatches, 1)

	args := oidcExtraArgs(t, kindConfig.Nodes[0].KubeadmConfigPatches[0])
	assert.Equal(t, v1alpha1.OIDCCAContainerPath, args["oidc-ca-file"])

	require.Len(t, kindConfig.Nodes[0].ExtraMounts, 1)
	mount := kindConfig.Nodes[0].ExtraMounts[0]
	assert.Equal(t, v1alpha1.OIDCCAContainerPath, mount.ContainerPath)
}

package configmanager_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFileWithParents writes content at path, creating any missing parent dirs.
func writeFileWithParents(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// newConfigManagerWithContext builds a ConfigManager whose cluster connection
// context is set, for testing context-derived name resolution helpers.
func newConfigManagerWithContext(context string) *configmanager.ConfigManager {
	mgr := configmanager.NewConfigManager(nil, "ksail.yaml")
	mgr.Config = &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Connection: v1alpha1.Connection{
					Context: context,
				},
			},
		},
	}

	return mgr
}

// TestIngressFirewallPatchPaths verifies the three expected patch file paths are
// constructed relative to the patches directory.
func TestIngressFirewallPatchPaths(t *testing.T) {
	t.Parallel()

	patchesDir := filepath.Join("some", "patches")

	defaultAction, cpRules, workerRules := configmanager.IngressFirewallPatchPathsForTest(
		patchesDir,
	)

	assert.Equal(
		t,
		filepath.Join(patchesDir, "cluster", "ingress-firewall-default-action.yaml"),
		defaultAction,
	)
	assert.Equal(
		t,
		filepath.Join(patchesDir, "control-planes", "ingress-firewall-rules.yaml"),
		cpRules,
	)
	assert.Equal(
		t,
		filepath.Join(patchesDir, "workers", "ingress-firewall-rules.yaml"),
		workerRules,
	)
}

// TestIngressFirewallPatchFilesExist verifies the helper only reports true when
// all three generated patch files are present on disk.
func TestIngressFirewallPatchFilesExist(t *testing.T) {
	t.Parallel()

	t.Run("all three present returns true", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		defaultAction, cpRules, workerRules := configmanager.IngressFirewallPatchPathsForTest(dir)
		writeFileWithParents(t, defaultAction, "a")
		writeFileWithParents(t, cpRules, "b")
		writeFileWithParents(t, workerRules, "c")

		assert.True(t, configmanager.IngressFirewallPatchFilesExistForTest(dir))
	})

	t.Run("missing worker rules returns false", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		defaultAction, cpRules, _ := configmanager.IngressFirewallPatchPathsForTest(dir)
		writeFileWithParents(t, defaultAction, "a")
		writeFileWithParents(t, cpRules, "b")

		assert.False(t, configmanager.IngressFirewallPatchFilesExistForTest(dir))
	})

	t.Run("empty directory returns false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, configmanager.IngressFirewallPatchFilesExistForTest(t.TempDir()))
	})
}

// TestKubeletPatchFilesExist verifies the helper only reports true when both
// kubelet patch files are present.
func TestKubeletPatchFilesExist(t *testing.T) {
	t.Parallel()

	certRotation := filepath.Join("cluster", "kubelet-cert-rotation.yaml")
	csrApprover := filepath.Join("cluster", "kubelet-csr-approver.yaml")

	t.Run("both present returns true", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeFileWithParents(t, filepath.Join(dir, certRotation), "a")
		writeFileWithParents(t, filepath.Join(dir, csrApprover), "b")

		assert.True(t, configmanager.KubeletPatchFilesExistForTest(dir))
	})

	t.Run("only cert rotation present returns false", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeFileWithParents(t, filepath.Join(dir, certRotation), "a")

		assert.False(t, configmanager.KubeletPatchFilesExistForTest(dir))
	})

	t.Run("none present returns false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, configmanager.KubeletPatchFilesExistForTest(t.TempDir()))
	})
}

// TestIngressFirewallPatchesErrors verifies input validation for the ingress
// firewall patch generator.
func TestIngressFirewallPatchesErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		networkCIDR string
		cniPort     int
		wantErr     error
	}{
		{
			name:        "empty CIDR",
			networkCIDR: "",
			cniPort:     8472,
			wantErr:     configmanager.ErrIngressFirewallMissingCIDRForTest,
		},
		{
			name:        "whitespace CIDR",
			networkCIDR: "   ",
			cniPort:     8472,
			wantErr:     configmanager.ErrIngressFirewallMissingCIDRForTest,
		},
		{
			name:        "invalid CIDR",
			networkCIDR: "not-a-cidr",
			cniPort:     8472,
			wantErr:     configmanager.ErrIngressFirewallInvalidCIDRForTest,
		},
		{
			name:        "port too low",
			networkCIDR: "10.0.0.0/8",
			cniPort:     0,
			wantErr:     configmanager.ErrIngressFirewallInvalidPortForTest,
		},
		{
			name:        "port too high",
			networkCIDR: "10.0.0.0/8",
			cniPort:     65536,
			wantErr:     configmanager.ErrIngressFirewallInvalidPortForTest,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			patches, err := configmanager.IngressFirewallPatchesForTest(
				testCase.networkCIDR,
				testCase.cniPort,
				nil,
			)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, patches)
		})
	}
}

// TestIngressFirewallPatchesSuccess verifies the generated patch set: three
// patches with the expected paths and scopes, the network CIDR normalized, and
// the control-plane rules honoring allowedCIDRs.
func TestIngressFirewallPatchesSuccess(t *testing.T) {
	t.Parallel()

	t.Run("no allowed CIDRs defaults to allow-all", func(t *testing.T) {
		t.Parallel()

		patches, err := configmanager.IngressFirewallPatchesForTest("10.244.0.0/16", 8472, nil)
		require.NoError(t, err)
		require.Len(t, patches, 3)

		assert.Equal(t, "ingress-firewall-default-action", patches[0].Path)
		assert.Equal(t, talosconfigmanager.PatchScopeCluster, patches[0].Scope)
		assert.Equal(t, "ingress-firewall-cp-rules", patches[1].Path)
		assert.Equal(t, talosconfigmanager.PatchScopeControlPlane, patches[1].Scope)
		assert.Equal(t, "ingress-firewall-worker-rules", patches[2].Path)
		assert.Equal(t, talosconfigmanager.PatchScopeWorker, patches[2].Scope)

		cpContent := string(patches[1].Content)
		assert.Contains(t, cpContent, "0.0.0.0/0")
		assert.Contains(t, cpContent, "10.244.0.0/16")
	})

	t.Run("allowed CIDRs restrict the control-plane rules", func(t *testing.T) {
		t.Parallel()

		patches, err := configmanager.IngressFirewallPatchesForTest(
			"10.244.0.0/16",
			8472,
			[]string{"192.168.1.0/24"},
		)
		require.NoError(t, err)
		require.Len(t, patches, 3)

		cpContent := string(patches[1].Content)
		assert.Contains(t, cpContent, "192.168.1.0/24")
		assert.NotContains(t, cpContent, "0.0.0.0/0")
	})

	t.Run("non-canonical network CIDR is normalized", func(t *testing.T) {
		t.Parallel()

		patches, err := configmanager.IngressFirewallPatchesForTest("192.168.1.5/24", 8472, nil)
		require.NoError(t, err)
		require.Len(t, patches, 3)

		cpContent := string(patches[1].Content)
		assert.Contains(t, cpContent, "192.168.1.0/24")
		assert.NotContains(t, cpContent, "192.168.1.5/24")
	})
}

// TestReadEKSConfigMetadata verifies the eksctl config metadata reader across
// missing-file, populated, metadata-less, and malformed-YAML cases.
func TestReadEKSConfigMetadata(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns empty values and no error", func(t *testing.T) {
		t.Parallel()

		path, name, region, err := configmanager.ReadEKSConfigMetadataForTest(
			filepath.Join(t.TempDir(), "absent.yaml"),
		)
		require.NoError(t, err)
		assert.Empty(t, path)
		assert.Empty(t, name)
		assert.Empty(t, region)
	})

	t.Run("populated metadata is parsed", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "eks.yaml")
		writeFileWithParents(t, configPath, "apiVersion: eksctl.io/v1alpha5\n"+
			"kind: ClusterConfig\n"+
			"metadata:\n"+
			"  name: my-eks\n"+
			"  region: eu-west-1\n")

		path, name, region, err := configmanager.ReadEKSConfigMetadataForTest(configPath)
		require.NoError(t, err)
		assert.Equal(t, "eks.yaml", filepath.Base(path))
		assert.Equal(t, "my-eks", name)
		assert.Equal(t, "eu-west-1", region)
	})

	t.Run("file without metadata returns empty name and region", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "eks.yaml")
		writeFileWithParents(t, configPath, "apiVersion: eksctl.io/v1alpha5\nkind: ClusterConfig\n")

		path, name, region, err := configmanager.ReadEKSConfigMetadataForTest(configPath)
		require.NoError(t, err)
		assert.NotEmpty(t, path)
		assert.Empty(t, name)
		assert.Empty(t, region)
	})

	for _, testCase := range []struct {
		name    string
		content string
	}{
		{
			name: "wrong API version is rejected",
			content: "apiVersion: v1\n" +
				"kind: ClusterConfig\n" +
				"metadata:\n  name: my-eks\n  region: eu-west-1\n",
		},
		{
			name: "wrong kind is rejected",
			content: "apiVersion: eksctl.io/v1alpha5\n" +
				"kind: ConfigMap\n" +
				"metadata:\n  name: my-eks\n  region: eu-west-1\n",
		},
		{
			name:    "missing API version is rejected",
			content: "kind: ClusterConfig\nmetadata:\n  name: my-eks\n  region: eu-west-1\n",
		},
		{
			name: "missing kind is rejected",
			content: "apiVersion: eksctl.io/v1alpha5\n" +
				"metadata:\n  name: my-eks\n  region: eu-west-1\n",
		},
		{
			name: "padded metadata name is rejected",
			content: "apiVersion: eksctl.io/v1alpha5\n" +
				"kind: ClusterConfig\n" +
				"metadata:\n  name: 'my-eks '\n  region: eu-west-1\n",
		},
		{
			name: "invalid metadata name is rejected",
			content: "apiVersion: eksctl.io/v1alpha5\n" +
				"kind: ClusterConfig\n" +
				"metadata:\n  name: Invalid_EKS\n  region: eu-west-1\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			configPath := filepath.Join(t.TempDir(), "eks.yaml")
			writeFileWithParents(t, configPath, testCase.content)

			_, _, _, err := configmanager.ReadEKSConfigMetadataForTest(configPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid EKS config file")
		})
	}

	t.Run("malformed YAML returns an error", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "eks.yaml")
		// A tab in the indentation is invalid YAML.
		writeFileWithParents(t, configPath, "metadata:\n\tname: my-eks\n")

		_, _, _, err := configmanager.ReadEKSConfigMetadataForTest(configPath)
		require.Error(t, err)
	})
}

// TestResolveKWOKName verifies KWOK cluster-name resolution from the kubeconfig
// context, with the default fallback.
func TestResolveKWOKName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
		want    string
	}{
		{name: "empty context returns default", context: "", want: "kwok-default"},
		{name: "kwok prefix extracts name", context: "kwok-my-cluster", want: "my-cluster"},
		{
			name:    "non-kwok context returns default",
			context: "kind-my-cluster",
			want:    "kwok-default",
		},
		{
			name:    "kwok prefix with empty name returns default",
			context: "kwok-",
			want:    "kwok-default",
		},
		{name: "surrounding whitespace is trimmed", context: "  kwok-trimmed  ", want: "trimmed"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mgr := newConfigManagerWithContext(testCase.context)
			assert.Equal(t, testCase.want, mgr.ResolveKWOKNameForTest())
		})
	}
}

// TestResolveEKSNameFromContext verifies EKS cluster-name extraction from an
// eksctl-style kubeconfig context, with the default fallback.
func TestResolveEKSNameFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
		want    string
	}{
		{name: "empty context returns default", context: "", want: "eks-default"},
		{
			name:    "full eksctl context extracts name",
			context: "iam-user@my-eks.eu-west-1.eksctl.io",
			want:    "my-eks",
		},
		{
			name:    "context without iam identity extracts name",
			context: "my-eks.eu-west-1.eksctl.io",
			want:    "my-eks",
		},
		{
			name:    "non-eksctl context returns default",
			context: "kind-my-cluster",
			want:    "eks-default",
		},
		{
			name:    "eksctl suffix with empty name returns default",
			context: "iam-user@.eksctl.io",
			want:    "eks-default",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mgr := newConfigManagerWithContext(testCase.context)
			assert.Equal(t, testCase.want, mgr.ResolveEKSNameFromContextForTest())
		})
	}
}

// TestReadGKEConfigSpec verifies the gke.yaml cluster-spec reader across
// missing-file, populated, and malformed cases.
func TestReadGKEConfigSpec(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns nil spec and no error", func(t *testing.T) {
		t.Parallel()

		path, spec, err := configmanager.ReadGKEConfigSpecForTest(
			filepath.Join(t.TempDir(), "absent.yaml"),
		)
		require.NoError(t, err)
		assert.Empty(t, path)
		assert.Nil(t, spec)
	})

	t.Run("populated spec is parsed via the proto JSON mapping", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "gke.yaml")
		writeFileWithParents(t, configPath, "name: my-gke\n"+
			"location: europe-north1\n"+
			"nodePools:\n"+
			"  - name: default\n"+
			"    initialNodeCount: 1\n")

		path, spec, err := configmanager.ReadGKEConfigSpecForTest(configPath)
		require.NoError(t, err)
		assert.Equal(t, "gke.yaml", filepath.Base(path))
		require.NotNil(t, spec)
		assert.Equal(t, "my-gke", spec.GetName())
		assert.Equal(t, "europe-north1", spec.GetLocation())
		require.Len(t, spec.GetNodePools(), 1)
		assert.Equal(t, "default", spec.GetNodePools()[0].GetName())
		assert.Equal(t, int32(1), spec.GetNodePools()[0].GetInitialNodeCount())
	})

	t.Run("unknown field returns an error", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "gke.yaml")
		writeFileWithParents(t, configPath, "name: my-gke\nnotAClusterField: true\n")

		_, _, err := configmanager.ReadGKEConfigSpecForTest(configPath)
		require.Error(t, err)
	})

	t.Run("malformed YAML returns an error", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "gke.yaml")
		writeFileWithParents(t, configPath, "name:\n\tmy-gke\n")

		_, _, err := configmanager.ReadGKEConfigSpecForTest(configPath)
		require.Error(t, err)
	})
}

// TestResolveGKENameFromContext verifies GKE cluster-name extraction from a
// gcloud-convention kubeconfig context, with the default fallback.
func TestResolveGKENameFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
		want    string
	}{
		{name: "empty context returns default", context: "", want: "gke-default"},
		{
			name:    "gcloud context extracts trailing name",
			context: "gke_my-project_europe-north1_my-cluster",
			want:    "my-cluster",
		},
		{name: "non-GKE context returns default", context: "kind-something", want: "gke-default"},
		{name: "prefix without name returns default", context: "gke_", want: "gke-default"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mgr := newConfigManagerWithContext(testCase.context)
			assert.Equal(t, testCase.want, mgr.ResolveGKENameFromContextForTest())
		})
	}
}

// TestReadAKSConfigSpec verifies the aks.yaml cluster-spec reader across
// missing-file, populated, unknown-field, and malformed cases.
func TestReadAKSConfigSpec(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns nil spec and no error", func(t *testing.T) {
		t.Parallel()

		path, spec, err := configmanager.ReadAKSConfigSpecForTest(
			filepath.Join(t.TempDir(), "absent.yaml"),
		)
		require.NoError(t, err)
		assert.Empty(t, path)
		assert.Nil(t, spec)
	})

	t.Run("populated spec is parsed via the ARM JSON mapping", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "aks.yaml")
		writeFileWithParents(t, configPath, "name: my-aks\n"+
			"location: swedencentral\n"+
			"properties:\n"+
			"  agentPoolProfiles:\n"+
			"    - name: default\n"+
			"      count: 1\n")

		path, spec, err := configmanager.ReadAKSConfigSpecForTest(configPath)
		require.NoError(t, err)
		assert.Equal(t, "aks.yaml", filepath.Base(path))
		require.NotNil(t, spec)
		require.NotNil(t, spec.Name)
		assert.Equal(t, "my-aks", *spec.Name)
		require.NotNil(t, spec.Location)
		assert.Equal(t, "swedencentral", *spec.Location)
		require.NotNil(t, spec.Properties)
		require.Len(t, spec.Properties.AgentPoolProfiles, 1)
		require.NotNil(t, spec.Properties.AgentPoolProfiles[0].Name)
		assert.Equal(t, "default", *spec.Properties.AgentPoolProfiles[0].Name)
	})

	t.Run("unknown field is tolerated by the ARM unmarshaler", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "aks.yaml")
		writeFileWithParents(t, configPath, "name: my-aks\nnotAClusterField: true\n")

		_, spec, err := configmanager.ReadAKSConfigSpecForTest(configPath)
		require.NoError(t, err)
		require.NotNil(t, spec)
		require.NotNil(t, spec.Name)
		assert.Equal(t, "my-aks", *spec.Name)
	})

	t.Run("malformed YAML returns an error", func(t *testing.T) {
		t.Parallel()

		configPath := filepath.Join(t.TempDir(), "aks.yaml")
		writeFileWithParents(t, configPath, "name:\n\tmy-aks\n")

		_, _, err := configmanager.ReadAKSConfigSpecForTest(configPath)
		require.Error(t, err)
	})
}

// TestResolveAKSNameFromContext verifies AKS cluster-name resolution from the
// kubeconfig context (az aks get-credentials names the context after the
// cluster itself), with the default fallback.
func TestResolveAKSNameFromContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
		want    string
	}{
		{name: "empty context returns default", context: "", want: "aks-default"},
		{name: "context is the cluster name", context: "my-aks", want: "my-aks"},
		{name: "whitespace context returns default", context: "   ", want: "aks-default"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mgr := newConfigManagerWithContext(testCase.context)
			assert.Equal(t, testCase.want, mgr.ResolveAKSNameFromContextForTest())
		})
	}
}

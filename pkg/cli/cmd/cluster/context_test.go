package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestResolveClusterNameFromContext_Vanilla(t *testing.T) {
	t.Parallel()

	kindConfig := &v1alpha4.Cluster{Name: "kind-cluster"}
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
				},
			},
		},
		KindConfig: kindConfig,
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "kind-cluster", name)
}

func TestResolveClusterNameFromContext_K3s(t *testing.T) {
	t.Parallel()

	k3dConfig := &v1alpha5.SimpleConfig{
		ObjectMeta: types.ObjectMeta{
			Name: "k3s-cluster",
		},
	}
	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionK3s,
				},
			},
		},
		K3dConfig: k3dConfig,
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "k3s-cluster", name)
}

func TestResolveClusterNameFromContext_EKS(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "stale-top-level-name"},
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{Distribution: v1alpha1.DistributionEKS},
			},
		},
		EKSConfig: &clusterprovisioner.EKSConfig{Name: "actual-eks-name"},
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "actual-eks-name", name)
}

func TestResolveClusterNameFromContext_FallbackToContext(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.Distribution("unknown"),
					Connection: v1alpha1.Connection{
						Context: "custom-context",
					},
				},
			},
		},
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "custom-context", name)
}

func TestResolveClusterNameFromContext_FallbackToDefault(t *testing.T) {
	t.Parallel()

	ctx := &localregistry.Context{
		ClusterCfg: &v1alpha1.Cluster{
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.Distribution("unknown"),
				},
			},
		},
	}

	name := cluster.ExportResolveClusterNameFromContext(ctx)
	require.Equal(t, "ksail", name)
}

func TestMatchesKindPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "exact control-plane match",
			containerName: "myapp-control-plane",
			clusterName:   "myapp",
			want:          true,
		},
		{
			name:          "exact worker match",
			containerName: "myapp-worker",
			clusterName:   "myapp",
			want:          true,
		},
		{
			name:          "numbered worker",
			containerName: "myapp-worker3",
			clusterName:   "myapp",
			want:          true,
		},
		{
			name:          "control-plane prefix mismatch",
			containerName: "myapp2-control-plane",
			clusterName:   "myapp",
			want:          false,
		},
		{
			name:          "control-plane for different cluster",
			containerName: "other-control-plane",
			clusterName:   "myapp",
			want:          false,
		},
		{
			name:          "K3d container not a Kind pattern",
			containerName: "k3d-myapp-server-0",
			clusterName:   "myapp",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportMatchesKindPattern(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsNumericString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "single digit", input: "0", want: true},
		{name: "multi digit", input: "123", want: true},
		{name: "large number", input: "9999", want: true},
		{name: "empty string", input: "", want: false},
		{name: "alpha string", input: "abc", want: false},
		{name: "alphanumeric", input: "1a2b", want: false},
		{name: "negative sign", input: "-1", want: false},
		{name: "whitespace", input: " 1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportIsNumericString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsCloudProviderKindContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		want          bool
	}{
		{
			name:          "exact ksail-cloud-provider-kind name",
			containerName: "ksail-cloud-provider-kind",
			want:          true,
		},
		{
			name:          "cpk-prefixed service container",
			containerName: "cpk-lb",
			want:          true,
		},
		{
			name:          "another cpk-prefixed container",
			containerName: "cpk-worker-1",
			want:          true,
		},
		{
			name:          "kind control-plane is not cloud-provider-kind",
			containerName: "dev-control-plane",
			want:          false,
		},
		{
			name:          "k3d server is not cloud-provider-kind",
			containerName: "k3d-dev-server-0",
			want:          false,
		},
		{
			name:          "empty string",
			containerName: "",
			want:          false,
		},
		{
			name:          "cloud-provider-kind substring in name",
			containerName: "old-ksail-cloud-provider-kind",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportIsCloudProviderKindContainer(tt.containerName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsKindClusterFromNodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		nodes       []string
		clusterName string
		want        bool
	}{
		{
			name:        "single control-plane node identifies Kind cluster",
			nodes:       []string{"dev-control-plane"},
			clusterName: "dev",
			want:        true,
		},
		{
			name:        "worker node also identifies Kind cluster",
			nodes:       []string{"dev-worker"},
			clusterName: "dev",
			want:        true,
		},
		{
			name:        "control-plane plus workers",
			nodes:       []string{"dev-control-plane", "dev-worker", "dev-worker2"},
			clusterName: "dev",
			want:        true,
		},
		{
			name:        "k3d nodes do not match Kind pattern",
			nodes:       []string{"k3d-dev-server-0", "k3d-dev-agent-0"},
			clusterName: "dev",
			want:        false,
		},
		{
			name:        "talos nodes do not match Kind pattern",
			nodes:       []string{"dev-controlplane-0", "dev-worker-0"},
			clusterName: "dev",
			want:        false,
		},
		{
			name:        "empty node list",
			nodes:       []string{},
			clusterName: "dev",
			want:        false,
		},
		{
			name:        "nil node list",
			nodes:       nil,
			clusterName: "dev",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportIsKindClusterFromNodes(tt.nodes, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesKindPattern_WorkerZero(t *testing.T) {
	t.Parallel()

	got := cluster.ExportMatchesKindPattern("mycluster-worker0", "mycluster")
	assert.True(t, got)
}

func TestMatchesKindPattern_WorkerWithMixedSuffix(t *testing.T) {
	t.Parallel()

	got := cluster.ExportMatchesKindPattern("mycluster-worker1a", "mycluster")
	assert.False(t, got)
}

func TestIsNumericString_SingleZero(t *testing.T) {
	t.Parallel()
	assert.True(t, cluster.ExportIsNumericString("0"))
}

func TestIsNumericString_Unicode(t *testing.T) {
	t.Parallel()
	assert.False(t, cluster.ExportIsNumericString("①②③"))
}

func TestIsKindClusterFromNodes_MultipleWorkers(t *testing.T) {
	t.Parallel()

	nodes := []string{
		"mycluster-control-plane",
		"mycluster-worker",
		"mycluster-worker2",
		"mycluster-worker3",
	}
	assert.True(t, cluster.ExportIsKindClusterFromNodes(nodes, "mycluster"))
}

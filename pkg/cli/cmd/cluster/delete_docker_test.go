package cluster_test

import (
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
)

func TestIsClusterContainer_KindControlPlane(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "exact control-plane match",
			containerName: "dev-control-plane",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "control-plane with different cluster name",
			containerName: "staging-control-plane",
			clusterName:   "staging",
			want:          true,
		},
		{
			name:          "control-plane prefix clash — 'dev' must not match 'dev2-control-plane'",
			containerName: "dev2-control-plane",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "control-plane suffix only — no cluster prefix",
			containerName: "-control-plane",
			clusterName:   "dev",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.IsClusterContainer(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsClusterContainer_KindWorkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "exact worker match",
			containerName: "dev-worker",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "numbered worker 2",
			containerName: "dev-worker2",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "numbered worker 10",
			containerName: "dev-worker10",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "worker with non-numeric suffix",
			containerName: "dev-workerabc",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "worker prefix clash — 'dev' must not match 'devprod-worker'",
			containerName: "devprod-worker",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "worker with alphanumeric suffix",
			containerName: "dev-worker2a",
			clusterName:   "dev",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.IsClusterContainer(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsClusterContainer_K3d(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "k3d server-0",
			containerName: "k3d-dev-server-0",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "k3d agent-0",
			containerName: "k3d-dev-agent-0",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "k3d server-1",
			containerName: "k3d-dev-server-1",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "k3d agent with multi-digit index",
			containerName: "k3d-dev-agent-10",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "k3d with different cluster name must not match",
			containerName: "k3d-staging-server-0",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "k3d prefix clash — 'dev' must not match 'k3d-dev2-server-0'",
			containerName: "k3d-dev2-server-0",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "k3d missing role segment",
			containerName: "k3d-dev-0",
			clusterName:   "dev",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.IsClusterContainer(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsClusterContainer_Talos(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "talos controlplane-0",
			containerName: "dev-controlplane-0",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "talos worker-0",
			containerName: "dev-worker-0",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "talos worker-1",
			containerName: "dev-worker-1",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "talos controlplane with different cluster",
			containerName: "staging-controlplane-0",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "talos prefix clash — 'dev' must not match 'dev2-controlplane-0'",
			containerName: "dev2-controlplane-0",
			clusterName:   "dev",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.IsClusterContainer(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsClusterContainer_VCluster(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "vcluster control plane container",
			containerName: "vcluster.cp.dev",
			clusterName:   "dev",
			want:          true,
		},
		{
			name:          "vcluster with different cluster name",
			containerName: "vcluster.cp.staging",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "vcluster prefix partial match must not match",
			containerName: "vcluster.cp.dev-extra",
			clusterName:   "dev",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.IsClusterContainer(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsClusterContainer_UnrelatedContainers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		clusterName   string
		want          bool
	}{
		{
			name:          "completely unrelated container",
			containerName: "nginx",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "empty container name",
			containerName: "",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "empty cluster name",
			containerName: "dev-control-plane",
			clusterName:   "",
			want:          false,
		},
		{
			name:          "cloud-provider-kind container",
			containerName: "ksail-cloud-provider-kind",
			clusterName:   "dev",
			want:          false,
		},
		{
			name:          "cpk-service container",
			containerName: "cpk-lb",
			clusterName:   "dev",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.IsClusterContainer(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
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

			got := clusterpkg.ExportMatchesKindPattern(tt.containerName, tt.clusterName)
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
		{name: "empty string", input: "", want: true}, // empty loops are vacuously true
		{name: "alpha string", input: "abc", want: false},
		{name: "alphanumeric", input: "1a2b", want: false},
		{name: "negative sign", input: "-1", want: false},
		{name: "whitespace", input: " 1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.ExportIsNumericString(tt.input)
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

			got := clusterpkg.ExportIsCloudProviderKindContainer(tt.containerName)
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

			got := clusterpkg.ExportIsKindClusterFromNodes(tt.nodes, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

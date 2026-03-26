package cluster_test

import (
	"testing"

	clusterpkg "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/stretchr/testify/assert"
)

type containerPatternTest struct {
	name          string
	containerName string
	clusterName   string
	want          bool
}

func dockerPatternTestCases() []containerPatternTest {
	return []containerPatternTest{
		// Kind control-plane cases
		{"kind: exact control-plane match", "dev-control-plane", "dev", true},
		{"kind: control-plane match with staging", "staging-control-plane", "staging", true},
		{"kind: control-plane prefix clash", "dev2-control-plane", "dev", false},
		{"kind: control-plane suffix only", "-control-plane", "dev", false},

		// Kind worker cases
		{"kind: exact worker match", "dev-worker", "dev", true},
		{"kind: numbered worker 2", "dev-worker2", "dev", true},
		{"kind: numbered worker 10", "dev-worker10", "dev", true},
		{"kind: worker with non-numeric suffix", "dev-workerabc", "dev", false},
		{"kind: worker prefix clash", "devprod-worker", "dev", false},
		{"kind: worker with alphanumeric suffix", "dev-worker2a", "dev", false},

		// K3d cases
		{"k3d: server-0", "k3d-dev-server-0", "dev", true},
		{"k3d: agent-0", "k3d-dev-agent-0", "dev", true},
		{"k3d: server-1", "k3d-dev-server-1", "dev", true},
		{"k3d: agent with multi-digit index", "k3d-dev-agent-10", "dev", true},
		{"k3d: different cluster", "k3d-staging-server-0", "dev", false},
		{"k3d: prefix clash", "k3d-dev2-server-0", "dev", false},
		{"k3d: missing role segment", "k3d-dev-0", "dev", false},

		// Talos cases
		{"talos: controlplane-0", "dev-controlplane-0", "dev", true},
		{"talos: worker-0", "dev-worker-0", "dev", true},
		{"talos: worker-1", "dev-worker-1", "dev", true},
		{"talos: controlplane different cluster", "staging-controlplane-0", "dev", false},
		{"talos: prefix clash", "dev2-controlplane-0", "dev", false},

		// VCluster cases
		{"vcluster: control plane", "vcluster.cp.dev", "dev", true},
		{"vcluster: different cluster", "vcluster.cp.staging", "dev", false},
		{"vcluster: prefix partial match", "vcluster.cp.dev-extra", "dev", false},

		// Unrelated containers
		{"unrelated: completely unrelated", "nginx", "dev", false},
		{"unrelated: empty container name", "", "dev", false},
		{"unrelated: empty cluster name", "dev-control-plane", "", false},
		{"unrelated: cloud-provider-kind", "ksail-cloud-provider-kind", "dev", false},
		{"unrelated: cpk-service", "cpk-lb", "dev", false},
	}
}

func TestIsClusterContainer_DockerPatterns(t *testing.T) {
	t.Parallel()

	for _, tt := range dockerPatternTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clusterpkg.IsClusterContainer(tt.containerName, tt.clusterName)
			assert.Equal(t, tt.want, got)
		})
	}
}

//nolint:dupl // structural similarity with restore_test.go is a false positive — different function under test
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
		{name: "empty string", input: "", want: false},
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

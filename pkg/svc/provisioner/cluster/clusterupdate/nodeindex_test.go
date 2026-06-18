package clusterupdate_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
)

//nolint:funlen // Table-driven allocation cases are clearest enumerated inline.
func TestAvailableNodeIndices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		names  []string
		prefix string
		base   int
		count  int
		want   []int
	}{
		// --- base 1 (Talos control-plane/worker series) ---
		{
			name:   "base1 empty list starts at 1",
			prefix: "c-control-plane-",
			base:   1,
			count:  1,
			want:   []int{1},
		},
		{
			name:   "base1 empty list multiple nodes are contiguous",
			prefix: "c-control-plane-",
			base:   1,
			count:  3,
			want:   []int{1, 2, 3},
		},
		{
			name:   "base1 no matching prefix",
			names:  []string{"other-control-plane-1", "c-worker-1"},
			prefix: "c-control-plane-",
			base:   1,
			count:  1,
			want:   []int{1},
		},
		{
			name:   "base1 contiguous extends past max",
			names:  []string{"c-control-plane-1", "c-control-plane-2", "c-control-plane-3"},
			prefix: "c-control-plane-",
			base:   1,
			count:  1,
			want:   []int{4},
		},
		{
			// Core #5312 behaviour at base 1.
			name:   "base1 reclaims a freed middle index",
			names:  []string{"c-control-plane-1", "c-control-plane-3"},
			prefix: "c-control-plane-",
			base:   1,
			count:  1,
			want:   []int{2},
		},
		{
			name:   "base1 fills gap then extends",
			names:  []string{"c-worker-1", "c-worker-3"},
			prefix: "c-worker-",
			base:   1,
			count:  2,
			want:   []int{2, 4},
		},
		{
			name:   "base1 picks lowest of multiple gaps",
			names:  []string{"c-worker-1", "c-worker-5"},
			prefix: "c-worker-",
			base:   1,
			count:  1,
			want:   []int{2},
		},
		{
			name:   "base1 ignores zero suffix below base",
			names:  []string{"c-control-plane-0", "c-control-plane-1"},
			prefix: "c-control-plane-",
			base:   1,
			count:  1,
			want:   []int{2},
		},
		{
			name:   "base1 ignores non-numeric suffix",
			names:  []string{"c-worker-abc", "c-worker-1"},
			prefix: "c-worker-",
			base:   1,
			count:  1,
			want:   []int{2},
		},
		// --- base 0 (k3d agent series) ---
		{
			name:   "base0 empty list starts at 0",
			prefix: "k3d-c-agent-",
			base:   0,
			count:  1,
			want:   []int{0},
		},
		{
			name:   "base0 empty list multiple nodes are contiguous from 0",
			prefix: "k3d-c-agent-",
			base:   0,
			count:  3,
			want:   []int{0, 1, 2},
		},
		{
			name:   "base0 reclaims a freed middle index",
			names:  []string{"k3d-c-agent-0", "k3d-c-agent-2"},
			prefix: "k3d-c-agent-",
			base:   0,
			count:  1,
			want:   []int{1},
		},
		{
			name:   "base0 fills gap then extends",
			names:  []string{"k3d-c-agent-0", "k3d-c-agent-2"},
			prefix: "k3d-c-agent-",
			base:   0,
			count:  2,
			want:   []int{1, 3},
		},
		{
			name:   "base0 reclaims leading gap at 0",
			names:  []string{"k3d-c-agent-1", "k3d-c-agent-2"},
			prefix: "k3d-c-agent-",
			base:   0,
			count:  1,
			want:   []int{0},
		},
		// --- guards ---
		{
			name:   "zero count yields empty",
			names:  []string{"c-worker-1"},
			prefix: "c-worker-",
			base:   1,
			count:  0,
			want:   []int{},
		},
		{
			name:   "negative count yields empty",
			prefix: "c-worker-",
			base:   1,
			count:  -2,
			want:   []int{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := clusterupdate.AvailableNodeIndices(
				testCase.names, testCase.prefix, testCase.base, testCase.count,
			)
			assert.Equal(t, testCase.want, got)
		})
	}
}

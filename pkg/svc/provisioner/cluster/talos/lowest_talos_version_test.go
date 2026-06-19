package talosprovisioner_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLowestTalosVersion verifies that the cluster's current Talos version is
// reported as the LEAST-upgraded node. A rolling Talos upgrade replaces nodes one
// at a time, so an interrupted upgrade leaves a mixed-version cluster; reconciling
// from the lowest version ensures the nodes still behind the target are rolled
// forward, instead of an already-upgraded node masking them and silently stalling
// the upgrade (the bug this guards against).
func TestLowestTalosVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tags []string
		want string
	}{
		{
			// Nodes are listed workers-first and upgraded first, so the already
			// upgraded workers (v1.13.4) precede the still-old control planes
			// (v1.13.3). The lowest — the version still to roll — must win, not the
			// first node sampled.
			name: "mixed cluster: upgraded workers must not mask old control planes",
			tags: []string{"v1.13.4", "v1.13.4", "v1.13.4", "v1.13.3", "v1.13.3", "v1.13.3"},
			want: "v1.13.3",
		},
		{
			name: "uniform cluster already at target",
			tags: []string{"v1.13.4", "v1.13.4", "v1.13.4"},
			want: "v1.13.4",
		},
		{
			name: "single node",
			tags: []string{"v1.13.3"},
			want: "v1.13.3",
		},
		{
			name: "lowest is last",
			tags: []string{"v1.13.4", "v1.13.3"},
			want: "v1.13.3",
		},
		{
			name: "patch ordering is numeric, not lexical",
			tags: []string{"v1.13.10", "v1.13.9"},
			want: "v1.13.9",
		},
		{
			name: "minor version difference",
			tags: []string{"v1.13.0", "v1.12.4"},
			want: "v1.12.4",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := talosprovisioner.LowestTalosVersionForTest(testCase.tags)
			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestLowestTalosVersionErrors verifies the helper fails loudly rather than
// silently under-reporting the cluster version.
func TestLowestTalosVersionErrors(t *testing.T) {
	t.Parallel()

	t.Run("empty input is version-undetermined", func(t *testing.T) {
		t.Parallel()

		_, err := talosprovisioner.LowestTalosVersionForTest(nil)
		require.ErrorIs(t, err, clustererr.ErrVersionUndetermined)
	})

	t.Run("unparseable version errors", func(t *testing.T) {
		t.Parallel()

		_, err := talosprovisioner.LowestTalosVersionForTest([]string{"v1.13.4", "not-a-version"})
		require.Error(t, err)
	})
}

// TestRunningVersionMatchesTarget verifies the per-node skip guard: during a
// rolling upgrade across a mixed-version cluster, a node already at the target is
// skipped (not rebooted), while a node behind the target is still upgraded.
func TestRunningVersionMatchesTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		running string
		target  string
		want    bool
	}{
		{name: "already at target", running: "v1.13.4", target: "v1.13.4", want: true},
		{name: "v-prefix mismatch still matches", running: "1.13.4", target: "v1.13.4", want: true},
		{name: "behind target is upgraded", running: "v1.13.3", target: "v1.13.4", want: false},
		{
			name:    "ahead of target is not skipped here",
			running: "v1.13.5",
			target:  "v1.13.4",
			want:    false,
		},
		{
			name:    "unparseable running errs toward upgrading",
			running: "garbage",
			target:  "v1.13.4",
			want:    false,
		},
		{
			name:    "unparseable target errs toward upgrading",
			running: "v1.13.4",
			target:  "garbage",
			want:    false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.RunningVersionMatchesTargetForTest(
				testCase.running,
				testCase.target,
			)
			assert.Equal(t, testCase.want, got)
		})
	}
}

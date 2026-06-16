package main

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

func snap(name string, phase v1alpha1.ClusterPhase) clusterSnapshot {
	return clusterSnapshot{Name: name, Phase: phase}
}

func TestClusterTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prev map[string]clusterSnapshot
		curr map[string]clusterSnapshot
		want []clusterTransition
	}{
		{
			name: "first observation is not reported",
			prev: map[string]clusterSnapshot{},
			curr: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseReady)},
			want: nil,
		},
		{
			name: "provisioning to ready is reported",
			prev: map[string]clusterSnapshot{
				"default/a": snap("a", v1alpha1.ClusterPhaseProvisioning),
			},
			curr: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseReady)},
			want: []clusterTransition{{Name: "a", Phase: v1alpha1.ClusterPhaseReady}},
		},
		{
			name: "updating to failed is reported",
			prev: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseUpdating)},
			curr: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseFailed)},
			want: []clusterTransition{{Name: "a", Phase: v1alpha1.ClusterPhaseFailed}},
		},
		{
			name: "unchanged phase is not reported",
			prev: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseReady)},
			curr: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseReady)},
			want: nil,
		},
		{
			name: "transition into a transient phase is not notable",
			prev: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseReady)},
			curr: map[string]clusterSnapshot{"default/a": snap("a", v1alpha1.ClusterPhaseUpdating)},
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := clusterTransitions(test.prev, test.curr)
			if len(got) != len(test.want) {
				t.Fatalf("got %d transitions, want %d: %+v", len(got), len(test.want), got)
			}

			for i := range test.want {
				if got[i] != test.want[i] {
					t.Errorf("transition %d = %+v, want %+v", i, got[i], test.want[i])
				}
			}
		})
	}
}

func TestBusyCount(t *testing.T) {
	t.Parallel()

	curr := map[string]clusterSnapshot{
		"default/a": snap("a", v1alpha1.ClusterPhaseProvisioning),
		"default/b": snap("b", v1alpha1.ClusterPhaseReady),
		"default/c": snap("c", v1alpha1.ClusterPhaseUpdating),
		"default/d": snap("d", v1alpha1.ClusterPhaseDeleting),
		"default/e": snap("e", v1alpha1.ClusterPhaseFailed),
	}

	if got := busyCount(curr); got != 3 {
		t.Errorf("busyCount = %d, want 3", got)
	}

	if got := busyCount(map[string]clusterSnapshot{}); got != 0 {
		t.Errorf("busyCount(empty) = %d, want 0", got)
	}
}

func TestTransitionNotification(t *testing.T) {
	t.Parallel()

	ready := transitionNotification(
		clusterTransition{Name: "demo", Phase: v1alpha1.ClusterPhaseReady},
	)
	if ready.Title != "Cluster ready" {
		t.Errorf("ready title = %q", ready.Title)
	}

	failed := transitionNotification(
		clusterTransition{Name: "demo", Phase: v1alpha1.ClusterPhaseFailed},
	)
	if failed.Title != "Cluster failed" {
		t.Errorf("failed title = %q", failed.Title)
	}

	if ready.ID == failed.ID {
		t.Errorf("notification IDs should differ by phase, both = %q", ready.ID)
	}
}

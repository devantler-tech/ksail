package cluster_test

import (
	"bytes"
	"strings"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const retainedLoadBalancersNotice = "existing Hetzner load balancers are left in place"

// loadBalancerToggle reconciles a lone cluster.loadBalancer change on Talos on
// Hetzner, with no values-drift change beside it, and returns what happened.
func loadBalancerToggle(
	t *testing.T,
	previous, desired v1alpha1.LoadBalancer,
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) (*clusterupdate.UpdateResult, string, error) {
	t.Helper()

	t.Cleanup(cluster.SetHcloudCCMInstallerFactoryForTests(factory))

	cfg := newHcloudCCMDriftContext().ClusterCfg
	cfg.Spec.Cluster.LoadBalancer = desired
	diff := &clusterupdate.UpdateResult{InPlaceChanges: []clusterupdate.Change{{
		Field:    "cluster.loadBalancer",
		OldValue: string(previous),
		NewValue: string(desired),
	}}}

	cmd := newReconcileTestCmd()

	var stderr bytes.Buffer

	cmd.SetErr(&stderr)

	result := clusterupdate.NewEmptyUpdateResult()
	err := cluster.ExportReconcileComponents(cmd, cfg, diff, result)

	return result, stderr.String(), err
}

// TestReconcileLoadBalancer_TogglesHcloudCCMOnTalosHetzner is ksail#7678: a
// cluster.loadBalancer change on Talos on Hetzner upgrades the cloud controller
// in both directions. Switching load balancers off used to fall through, change
// nothing and still record the change as applied.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileLoadBalancer_TogglesHcloudCCMOnTalosHetzner(t *testing.T) {
	for name, transition := range map[string]struct {
		from, to   v1alpha1.LoadBalancer
		wantNotice bool
	}{
		"enabled to disabled": {
			from: v1alpha1.LoadBalancerEnabled, to: v1alpha1.LoadBalancerDisabled, wantNotice: true,
		},
		"default to disabled": {
			from: v1alpha1.LoadBalancerDefault, to: v1alpha1.LoadBalancerDisabled, wantNotice: true,
		},
		"disabled to enabled": {from: v1alpha1.LoadBalancerDisabled, to: v1alpha1.LoadBalancerEnabled},
		"disabled to default": {from: v1alpha1.LoadBalancerDisabled, to: v1alpha1.LoadBalancerDefault},
	} {
		t.Run(name, func(t *testing.T) {
			inst := &driftingInstaller{}

			result, stderr, err := loadBalancerToggle(t, transition.from, transition.to,
				func(*v1alpha1.Cluster) (installer.Installer, error) { return inst, nil })

			require.NoError(t, err)
			assert.Equal(t, 1, inst.installs, "the cloud controller must be upgraded once")
			require.Len(t, result.AppliedChanges, 1)
			assert.Equal(t, "cluster.loadBalancer", result.AppliedChanges[0].Field)
			assert.Equal(
				t,
				transition.wantNotice,
				strings.Contains(stderr, retainedLoadBalancersNotice),
				"stderr: %s",
				stderr,
			)
		})
	}
}

// TestReconcileLoadBalancer_FailedHcloudCCMDisableIsReported keeps a failed
// upgrade from being recorded as a successful switch to Disabled.
//
//nolint:paralleltest // overrides the process-wide installer factories.
func TestReconcileLoadBalancer_FailedHcloudCCMDisableIsReported(t *testing.T) {
	result, stderr, err := loadBalancerToggle(
		t,
		v1alpha1.LoadBalancerEnabled,
		v1alpha1.LoadBalancerDisabled,
		func(*v1alpha1.Cluster) (installer.Installer, error) { return nil, errValuesProbeFailed },
	)

	require.ErrorIs(t, err, errValuesProbeFailed)
	assert.Empty(t, result.AppliedChanges)
	require.Len(t, result.FailedChanges, 1)
	assert.Equal(t, "cluster.loadBalancer", result.FailedChanges[0].Field)
	assert.NotContains(t, stderr, retainedLoadBalancersNotice, "no warning when the switch failed")
}

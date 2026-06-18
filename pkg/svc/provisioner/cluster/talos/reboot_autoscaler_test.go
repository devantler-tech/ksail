package talosprovisioner_test

import (
	"context"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAutoscalerRebootRequired pins the reboot-in-place gate: only a reboot-required
// diff routes to the in-place reboot path; image/wipe/recreate/rolling-recreate are
// recycle's job (asserted in TestAutoscalerRecycleRequired) and must not be reboot.
func TestAutoscalerRebootRequired(t *testing.T) {
	t.Parallel()

	rebootDiff := clusterupdate.NewEmptyUpdateResult()
	rebootDiff.RebootRequired = append(rebootDiff.RebootRequired, clusterupdate.Change{})

	wipeDiff := clusterupdate.NewEmptyUpdateResult()
	wipeDiff.WipeRequired = append(wipeDiff.WipeRequired, clusterupdate.Change{})

	recreateDiff := clusterupdate.NewEmptyUpdateResult()
	recreateDiff.RecreateRequired = append(recreateDiff.RecreateRequired, clusterupdate.Change{})

	rollingDiff := clusterupdate.NewEmptyUpdateResult()
	rollingDiff.RollingRecreate = append(rollingDiff.RollingRecreate, clusterupdate.Change{})

	tests := []struct {
		name string
		diff *clusterupdate.UpdateResult
		want bool
	}{
		{"nil diff is not reboot", nil, false},
		{"empty diff is not reboot", clusterupdate.NewEmptyUpdateResult(), false},
		{"reboot-required is reboot", rebootDiff, true},
		{"wipe-required is not reboot", wipeDiff, false},
		{"recreate-required is not reboot", recreateDiff, false},
		{"rolling-recreate is not reboot", rollingDiff, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.AutoscalerRebootRequiredForTest(testCase.diff)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestRollingRebootAutoscalerNodes_NoopWhenNotHetzner(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(io.Discard)

	err := prov.RollingRebootAutoscalerNodesForTest(
		context.Background(), "test-cluster", clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
}

func TestRollingRebootAutoscalerNodes_NoopWhenAutoscalerDisabled(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   false,
			AutoscalerNodePoolNames: []string{"pool-a"},
		})

	err := prov.RollingRebootAutoscalerNodesForTest(
		context.Background(), "test-cluster", clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
}

func TestRollingRebootAutoscalerNodes_NoopWhenNoPools(t *testing.T) {
	t.Parallel()

	prov := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(io.Discard).
		WithHetznerOptions(v1alpha1.OptionsHetzner{
			NodeAutoscalerEnabled:   true,
			AutoscalerNodePoolNames: nil,
		})

	err := prov.RollingRebootAutoscalerNodesForTest(
		context.Background(), "test-cluster", clusterupdate.NewEmptyUpdateResult(),
	)
	require.NoError(t, err)
}

package talosprovisioner

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
)

// A same-version image roll refreshes the Secret before the regular update
// computes its diff. The later classified pass must still converge existing
// autoscaler nodes when the earlier refresh left the Secret unchanged.
func TestShouldPropagateAutoscalerBaselineAfterEarlySecretRefresh(t *testing.T) {
	t.Parallel()

	reboot := clusterupdate.NewEmptyUpdateResult()
	reboot.RebootRequired = append(reboot.RebootRequired, clusterupdate.Change{})
	wipe := clusterupdate.NewEmptyUpdateResult()
	wipe.WipeRequired = append(wipe.WipeRequired, clusterupdate.Change{})

	for _, testCase := range []struct {
		name    string
		changed bool
		diff    *clusterupdate.UpdateResult
		want    bool
	}{
		{"early refresh with unknown diff", true, nil, true},
		{"unchanged Secret with reboot-required diff", false, reboot, true},
		{"unchanged Secret with wipe-required diff", false, wipe, true},
		{"unchanged Secret with in-place diff", false, clusterupdate.NewEmptyUpdateResult(), false},
		{"unchanged Secret before diff", false, nil, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(
				t,
				testCase.want,
				shouldPropagateAutoscalerBaseline(testCase.changed, testCase.diff),
			)
		})
	}
}

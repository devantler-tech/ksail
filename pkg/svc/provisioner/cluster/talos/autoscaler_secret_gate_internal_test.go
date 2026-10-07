package talosprovisioner

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/stretchr/testify/assert"
)

// A same-version image roll refreshes the Secret before the regular update
// computes its diff. The later classified pass must still converge existing
// autoscaler nodes when that earlier refresh left the Secret unchanged — and
// must leave them alone when no pass of this update changed the Secret.
func TestShouldPropagateAutoscalerBaselineAfterEarlySecretRefresh(t *testing.T) {
	t.Parallel()

	reboot := clusterupdate.NewEmptyUpdateResult()
	reboot.RebootRequired = append(reboot.RebootRequired, clusterupdate.Change{})
	wipe := clusterupdate.NewEmptyUpdateResult()
	wipe.WipeRequired = append(wipe.WipeRequired, clusterupdate.Change{})
	inPlace := clusterupdate.NewEmptyUpdateResult()
	inPlace.InPlaceChanges = append(inPlace.InPlaceChanges, clusterupdate.Change{})

	for _, testCase := range []struct {
		name           string
		changed        bool
		refreshedEarly bool
		diff           *clusterupdate.UpdateResult
		want           bool
	}{
		{"changed Secret with empty diff", true, false, clusterupdate.NewEmptyUpdateResult(), true},
		{"early refresh with reboot-required diff", false, true, reboot, true},
		{"early refresh with wipe-required diff", false, true, wipe, true},
		{"early refresh with in-place diff", false, true, inPlace, true},
		{"early refresh with empty diff", false, true, clusterupdate.NewEmptyUpdateResult(), false},
		{"no refresh with reboot-required diff", false, false, reboot, false},
		{"no refresh with wipe-required diff", false, false, wipe, false},
		{"no refresh with in-place diff", false, false, inPlace, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(
				t,
				testCase.want,
				shouldPropagateAutoscalerBaseline(
					testCase.changed, testCase.refreshedEarly, testCase.diff,
				),
			)
		})
	}
}

// The earlier pass of an update has no diff; the classified pass consumes what it
// recorded exactly once, so a later update on the same provisioner starts clean.
func TestAutoscalerSecretChangedThisUpdateRemembersEarlyRefreshOnce(t *testing.T) {
	t.Parallel()

	reboot := clusterupdate.NewEmptyUpdateResult()
	reboot.RebootRequired = append(reboot.RebootRequired, clusterupdate.Change{})

	prov := &Provisioner{}

	assert.True(t, prov.autoscalerSecretChangedThisUpdate(true, nil))
	assert.True(t, prov.autoscalerSecretChangedThisUpdate(false, reboot))
	assert.False(t, prov.autoscalerSecretChangedThisUpdate(false, reboot))

	assert.False(t, prov.autoscalerSecretChangedThisUpdate(false, nil))
	assert.False(t, prov.autoscalerSecretChangedThisUpdate(false, reboot))
}

package talosprovisioner_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The distinctive no-op log line each propagateAutoscalerBaseline route prints when
// there are no autoscaler nodes — what lets the routing decision be asserted with no
// live infra.
const (
	recycleNoopMsg = "No autoscaler nodes to recycle"
	rebootNoopMsg  = "No autoscaler nodes to reboot"
	inPlaceNoopMsg = "No autoscaler nodes to reconcile"
)

type propagateRoutingCase struct {
	name         string
	diff         *clusterupdate.UpdateResult
	imageChanged bool
	wantMsg      string
}

// propagateRoutingCases enumerates the diff shapes and the convergence path each must
// route to. Recycle (a fresh server) is reserved for an image bump or a
// wipe/recreate/rolling-recreate diff; a reboot-required diff converges in place on
// the SAME server (the capacity-constrained #5219 recurrence the in-place reboot path
// was added for); everything else takes the NO_REBOOT in-place apply.
func propagateRoutingCases() []propagateRoutingCase {
	diffWith := func(set func(*clusterupdate.UpdateResult)) *clusterupdate.UpdateResult {
		diff := clusterupdate.NewEmptyUpdateResult()
		set(diff)

		return diff
	}

	return []propagateRoutingCase{
		{"image bump recycles", inPlaceDiff(), true, recycleNoopMsg},
		{"wipe-required recycles", diffWith(func(d *clusterupdate.UpdateResult) {
			d.WipeRequired = append(d.WipeRequired, clusterupdate.Change{})
		}), false, recycleNoopMsg},
		{"recreate-required recycles", diffWith(func(d *clusterupdate.UpdateResult) {
			d.RecreateRequired = append(d.RecreateRequired, clusterupdate.Change{})
		}), false, recycleNoopMsg},
		{"rolling-recreate recycles", diffWith(func(d *clusterupdate.UpdateResult) {
			d.RollingRecreate = append(d.RollingRecreate, clusterupdate.Change{})
		}), false, recycleNoopMsg},
		{"reboot-required reboots in place", diffWith(func(d *clusterupdate.UpdateResult) {
			d.RebootRequired = append(d.RebootRequired, clusterupdate.Change{})
		}), false, rebootNoopMsg},
		{"reboot-required with image bump recycles", diffWith(func(d *clusterupdate.UpdateResult) {
			d.RebootRequired = append(d.RebootRequired, clusterupdate.Change{})
		}), true, recycleNoopMsg},
		{"in-place only applies in place", inPlaceDiff(), false, inPlaceNoopMsg},
		{"nil diff defaults to in place", nil, false, inPlaceNoopMsg},
		{
			"empty diff defaults to in place",
			clusterupdate.NewEmptyUpdateResult(),
			false,
			inPlaceNoopMsg,
		},
	}
}

// TestPropagateAutoscalerBaseline_Routing pins the #5219 dispatcher: each diff shape
// must reach the correct convergence path. With the autoscaler enabled but no pools
// configured, listAutoscalerServers short-circuits to zero servers without ever
// contacting Hetzner, so each route reaches its distinctive no-op log line — which is
// how the routing decision is identified deterministically.
func TestPropagateAutoscalerBaseline_Routing(t *testing.T) {
	t.Parallel()

	for _, testCase := range propagateRoutingCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			prov := talosprovisioner.NewProvisioner(nil, nil).
				WithLogWriter(&buf).
				WithHetznerOptions(v1alpha1.OptionsHetzner{
					NodeAutoscalerEnabled:   true,
					AutoscalerNodePoolNames: nil,
				})

			err := prov.PropagateAutoscalerBaselineForTest(
				context.Background(),
				"test-cluster",
				testCase.diff,
				testCase.imageChanged,
				clusterupdate.NewEmptyUpdateResult(),
			)
			require.NoError(t, err)
			assert.Contains(t, buf.String(), testCase.wantMsg)
		})
	}
}

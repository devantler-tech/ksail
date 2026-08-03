package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// fluxVerifyField is the diff field name for artifact-verification drift.
const fluxVerifyField = "cluster.workload.flux.verify"

// TestFluxVerifyFieldHasReconcileHandler pins the wiring between detection and
// application, which is the half platform#2922 was actually missing. The verify
// block was configured, read by KSail, and never applied: nothing on the update
// path claimed the field, so the change would be skipped and the update would
// still report success against an unverified root source.
func TestFluxVerifyFieldHasReconcileHandler(t *testing.T) {
	t.Parallel()

	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux

	assert.True(
		t,
		cluster.ExportHandlerForField(&cobra.Command{}, clusterCfg, fluxVerifyField),
		"no reconcile handler is registered for %q, so detected verify drift would be silently skipped",
		fluxVerifyField,
	)
}

// TestFluxVerifyDriftStaysInPlace is the trap this change most has to avoid.
// promoteUnsupportedInPlaceChanges demotes any in-place field the provisioner
// does not declare support for to "recreate required" — so a field missing from
// the component-reconcile set would turn re-asserting a signature policy into a
// demand to destroy and rebuild the cluster. On the source this fix targets that
// cluster is production.
func TestFluxVerifyDriftStaysInPlace(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = []clusterupdate.Change{
		{
			Field:    fluxVerifyField,
			Category: clusterupdate.ChangeCategoryInPlace,
		},
	}

	// An updater that declares support for nothing: only membership of the
	// component-reconcile set can keep the change in place.
	updater := &fieldSupportUpdater{
		fakeUpdater: &fakeUpdater{},
		supported:   map[string]bool{},
	}

	cluster.ExportPromoteUnsupportedInPlaceChanges(updater, diff)

	assert.Empty(
		t,
		diff.RecreateRequired,
		"re-asserting artifact verification must never demand a cluster rebuild",
	)
	assert.Len(
		t,
		diff.InPlaceChanges,
		1,
		"the verify change must survive promotion as an in-place change",
	)
}

package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		cluster.ExportHandlerForField(&cobra.Command{}, clusterCfg, specdiff.FluxVerifyField),
		"no reconcile handler is registered for %q, so detected verify drift would be silently skipped",
		specdiff.FluxVerifyField,
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
			Field:    specdiff.FluxVerifyField,
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

// TestFluxReassertRunsOncePerUpdatePass pins the coalescing of the two Flux
// handlers.
//
// spec.workload.flux.distributionVersion and spec.workload.flux.verify are
// separate diff fields with separate handlers, but both repair themselves
// through the same SetupInstance upsert. An update reporting both would
// otherwise run that upsert twice — the same duplication the autoscaler and
// load-balancer flags already prevent for their own fields.
func TestFluxReassertRunsOncePerUpdatePass(t *testing.T) {
	t.Parallel()

	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineNone

	first, second := cluster.ExportFluxReassertMemoized(&cobra.Command{}, clusterCfg)

	require.NoError(t, first, "a non-Flux cluster must reassert cleanly as a no-op")
	require.NoError(
		t,
		second,
		"the second Flux handler must reuse the first reassertion; recomputing would "+
			"have attempted the upsert against a cancelled context",
	)
}

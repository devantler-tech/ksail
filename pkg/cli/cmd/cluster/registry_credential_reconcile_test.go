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

// TestRegistryCredentialFieldHasReconcileHandler pins the wiring between
// detection and application. Detecting credential drift is useless if no
// handler claims the field: reconcileComponents would skip it and the revoked
// credential would stay in the cluster while the update reported success.
func TestRegistryCredentialFieldHasReconcileHandler(t *testing.T) {
	t.Parallel()

	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux

	assert.True(t,
		cluster.ExportHandlerForField(&cobra.Command{}, clusterCfg, specdiff.RegistryCredentialField),
		"no reconcile handler is registered for %q, so detected credential drift would be silently skipped",
		specdiff.RegistryCredentialField,
	)
}

// TestRegistryCredentialDriftStaysInPlace is the trap this change most has to
// avoid. promoteUnsupportedInPlaceChanges demotes any in-place field the
// provisioner does not declare support for to "recreate required" — so a field
// missing from the component-reconcile set turns a routine token rotation into
// a demand to destroy and rebuild the cluster.
func TestRegistryCredentialDriftStaysInPlace(t *testing.T) {
	t.Parallel()

	diff := clusterupdate.NewEmptyUpdateResult()
	diff.InPlaceChanges = []clusterupdate.Change{
		{
			Field:    specdiff.RegistryCredentialField,
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

	require.Len(t, diff.InPlaceChanges, 1,
		"a credential rotation must remain an in-place change")
	assert.Equal(t, specdiff.RegistryCredentialField, diff.InPlaceChanges[0].Field)
	assert.Empty(t, diff.RecreateRequired,
		"a credential rotation must never be promoted to cluster recreation")
}

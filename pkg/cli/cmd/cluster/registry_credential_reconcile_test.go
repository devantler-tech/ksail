package cluster_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
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

	assert.True(
		t,
		cluster.ExportHandlerForField(
			&cobra.Command{},
			clusterCfg,
			specdiff.RegistryCredentialField,
		),
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

// specOnlyDiffRanRegistryCredentialCheck reports whether computeSpecOnlyDiff reached
// checkRegistryCredentialDrift, judged by the signals that check can leave behind: one of
// its own warnings, or a credential change on the diff.
//
// The kubeconfig points at a file that does not exist, so no test run ever reads a real
// cluster: the check stops at its comparison warning. That keeps the property under test —
// which diff path invokes the check — provable without a cluster.
func specOnlyDiffRanRegistryCredentialCheck(t *testing.T, clusterCfg *v1alpha1.Cluster) bool {
	t.Helper()

	clusterCfg.Spec.Cluster.Connection.Kubeconfig = filepath.Join(t.TempDir(), "missing-kubeconfig")

	var out bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	diff := cluster.ExportComputeSpecOnlyDiff(cmd, &localregistry.Context{ClusterCfg: clusterCfg})

	// Both warnings are unique to the credential check: every sibling drift warning names
	// the distribution version, the sync ref, or artifact verification instead.
	warned := strings.Contains(out.String(), "registry credential drift detection") ||
		strings.Contains(out.String(), "compare registry credentials for drift detection")

	changed := false

	for _, change := range diff.InPlaceChanges {
		if change.Field == specdiff.RegistryCredentialField {
			changed = true
		}
	}

	return warned || changed
}

// fluxClusterWithExternalRegistryCredentials builds a Flux cluster that pulls from an
// external registry with inline credentials — the configuration whose rotation only the
// credential check can see.
func fluxClusterWithExternalRegistryCredentials() *v1alpha1.Cluster {
	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	clusterCfg.Spec.Cluster.LocalRegistry.Registry = "ksail-bot:a-token@ghcr.io/devantler-tech/repo"

	return clusterCfg
}

// TestSpecOnlyDiffChecksRegistryCredentialDrift pins the fix. checkRegistryCredentialDrift
// was wired into the Updater diff path only. Registry passwords are redacted from the
// structural diff, so a credential-only rotation produces no field change: `ksail cluster
// diff` previewed nothing while `cluster update` refreshed the credential, and provisioners
// with no Updater (VCluster) kept authenticating with the revoked value.
func TestSpecOnlyDiffChecksRegistryCredentialDrift(t *testing.T) {
	t.Parallel()

	assert.True(
		t,
		specOnlyDiffRanRegistryCredentialCheck(t, fluxClusterWithExternalRegistryCredentials()),
		"computeSpecOnlyDiff must run checkRegistryCredentialDrift, so `cluster diff` previews "+
			"the same credential refresh `cluster update` applies",
	)
}

// TestSpecOnlyDiffSkipsRegistryCredentialDriftWhenNotApplicable pins the guards, so wiring
// the check into a second path cannot make it fire where it must stay silent.
func TestSpecOnlyDiffSkipsRegistryCredentialDriftWhenNotApplicable(t *testing.T) {
	t.Parallel()

	argocd := fluxClusterWithExternalRegistryCredentials()
	argocd.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineArgoCD

	assert.False(
		t,
		specOnlyDiffRanRegistryCredentialCheck(t, argocd),
		"the registry Secret is Flux's root pull Secret; an ArgoCD cluster must not be queried for it",
	)

	noCredentials := v1alpha1.NewCluster()
	noCredentials.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	noCredentials.Spec.Cluster.LocalRegistry.Registry = "ghcr.io/devantler-tech/repo"

	assert.False(
		t,
		specOnlyDiffRanRegistryCredentialCheck(t, noCredentials),
		"an external registry with no credentials has nothing to refresh, and no cluster query worth spending",
	)

	local := v1alpha1.NewCluster()
	local.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux

	assert.False(
		t,
		specOnlyDiffRanRegistryCredentialCheck(t, local),
		"a default local registry is not external, so there is no credential to compare",
	)
}

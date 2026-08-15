package cluster_test

import (
	"bytes"
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

// specOnlyDiffRanVerifyCheck reports whether computeSpecOnlyDiff reached
// checkFluxVerifyDrift, judged by the two signals that check can leave behind: one of its
// own warnings, or a verify change on the diff when the cluster query actually succeeded.
//
// Keying on the check's observable effect rather than on a live cluster is deliberate —
// the property under test is which diff path invokes the check, and that must be provable
// without a cluster.
func specOnlyDiffRanVerifyCheck(t *testing.T, clusterCfg *v1alpha1.Cluster) bool {
	t.Helper()

	var out bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	diff := cluster.ExportComputeSpecOnlyDiff(cmd, &localregistry.Context{ClusterCfg: clusterCfg})

	// Both warnings are unique to the verify check: every sibling drift warning names the
	// distribution version, the sync ref, or registry credentials instead.
	warned := strings.Contains(out.String(), "Flux verify drift detection") ||
		strings.Contains(out.String(), "Flux artifact verification")

	changed := false

	for _, change := range diff.InPlaceChanges {
		if change.Field == specdiff.FluxVerifyField {
			changed = true
		}
	}

	return warned || changed
}

// fluxClusterWithVerify builds a Flux cluster whose verification is configured but has not
// been applied — the exact state platform#2922 sat in.
func fluxClusterWithVerify() *v1alpha1.Cluster {
	clusterCfg := v1alpha1.NewCluster()
	clusterCfg.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux
	clusterCfg.Spec.Workload.Flux.Verify = v1alpha1.FluxVerifySpec{Provider: "cosign"}

	return clusterCfg
}

// TestSpecOnlyDiffChecksFluxVerifyDrift pins the fix. checkFluxVerifyDrift was wired into
// the Updater diff path only, while its two sibling checks were wired into both — so
// `ksail cluster diff` previewed an update without the verification repair that
// `cluster update` would then apply, and provisioners with no Updater (VCluster) never
// detected the drift at all. A preview that silently omits a security-relevant repair is
// worse than no preview, because it is trusted.
func TestSpecOnlyDiffChecksFluxVerifyDrift(t *testing.T) {
	t.Parallel()

	assert.True(
		t,
		specOnlyDiffRanVerifyCheck(t, fluxClusterWithVerify()),
		"computeSpecOnlyDiff must run checkFluxVerifyDrift, so `cluster diff` previews the "+
			"same verification repair `cluster update` applies",
	)
}

// TestSpecOnlyDiffSkipsFluxVerifyDriftWhenNotApplicable pins the guards, so wiring the
// check into a second path cannot make it fire where it must stay silent.
func TestSpecOnlyDiffSkipsFluxVerifyDriftWhenNotApplicable(t *testing.T) {
	t.Parallel()

	argocd := fluxClusterWithVerify()
	argocd.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineArgoCD

	assert.False(
		t,
		specOnlyDiffRanVerifyCheck(t, argocd),
		"artifact verification is a Flux concern; an ArgoCD cluster must not be queried for it",
	)

	unconfigured := v1alpha1.NewCluster()
	unconfigured.Spec.Cluster.GitOpsEngine = v1alpha1.GitOpsEngineFlux

	assert.False(
		t,
		specOnlyDiffRanVerifyCheck(t, unconfigured),
		"with no verify block configured there is nothing to assert, and no cluster query worth spending",
	)
}

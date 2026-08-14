package diff_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

func newVerifyEngine() *specdiff.Engine {
	return specdiff.NewEngine(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
}

// TestCheckFluxVerifySurfacesAnUnverifiedCluster is the core of platform#2922:
// spec.workload.flux.verify is applied to the cluster only by SetupInstance, and
// cluster update reaches that solely through handlers other fields' changes
// trigger. A cluster whose live OCIRepository carries no spec.verify must
// therefore surface as an in-place change, or verification stays configured and
// unenforced behind a green deploy.
func TestCheckFluxVerifySurfacesAnUnverifiedCluster(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newVerifyEngine().CheckFluxVerify(true, v1alpha1.GitOpsEngineFlux, result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf(
			"an unverified live OCIRepository must produce exactly one in-place change, got %d",
			len(result.InPlaceChanges),
		)
	}

	change := result.InPlaceChanges[0]
	if change.Field != specdiff.FluxVerifyField {
		t.Errorf("field = %q, want %q", change.Field, specdiff.FluxVerifyField)
	}

	if change.Category != clusterupdate.ChangeCategoryInPlace {
		t.Errorf(
			"category = %q, want in-place — re-asserting verify must never demand recreation",
			change.Category,
		)
	}
}

// TestCheckFluxVerifyDoesNotClaimAKnownLiveState pins the reported old value
// against the one thing the detector cannot know.
//
// VerifyDrifted returns a single boolean for two different live states: no
// spec.verify block at all, and a block that is present but differs from the
// configured one. The update plan is read by an operator against their own
// cluster, so an old value naming just one of those states is a false statement
// for everyone in the other. The label must therefore cover both.
func TestCheckFluxVerifyDoesNotClaimAKnownLiveState(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newVerifyEngine().CheckFluxVerify(true, v1alpha1.GitOpsEngineFlux, result)

	// Check the length before indexing: a detector that emitted nothing is the
	// very regression this test exists to catch, and an unguarded index reports
	// it as a panic in the harness rather than as this test's own failure.
	if len(result.InPlaceChanges) != 1 {
		t.Fatalf("expected exactly one in-place change, got %d", len(result.InPlaceChanges))
	}

	old := result.InPlaceChanges[0].OldValue

	// The specific state a live differing block would contradict. Named
	// explicitly so a revert to it fails here rather than only shifting a
	// string somewhere.
	if old == "absent" {
		t.Fatalf(
			"old value = %q, which is false for a cluster whose live spec.verify "+
				"is present but uses another provider",
			old,
		)
	}

	if !strings.Contains(old, "absent") || !strings.Contains(old, "mismatched") {
		t.Errorf(
			"old value = %q, want a label naming both drift causes (absent, mismatched)",
			old,
		)
	}
}

// TestCheckFluxVerifyStaysSilentWhenTheClusterAlreadyVerifies pins the other
// direction. Without this, a check that emitted unconditionally would pass the
// test above while making every single update report a spurious change.
func TestCheckFluxVerifyStaysSilentWhenTheClusterAlreadyVerifies(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newVerifyEngine().CheckFluxVerify(false, v1alpha1.GitOpsEngineFlux, result)

	if len(result.InPlaceChanges) != 0 {
		t.Fatalf(
			"a cluster already carrying the configured verify block must produce no change, got %d",
			len(result.InPlaceChanges),
		)
	}
}

// TestCheckFluxVerifyIgnoresNonFluxEngines keeps the check scoped. ArgoCD has no
// OCIRepository to patch, so emitting a change there would route to a Flux-only
// handler.
func TestCheckFluxVerifyIgnoresNonFluxEngines(t *testing.T) {
	t.Parallel()

	for _, engine := range []v1alpha1.GitOpsEngine{
		v1alpha1.GitOpsEngineArgoCD,
		v1alpha1.GitOpsEngineNone,
	} {
		result := clusterupdate.NewEmptyUpdateResult()
		newVerifyEngine().CheckFluxVerify(true, engine, result)

		if len(result.InPlaceChanges) != 0 {
			t.Errorf(
				"engine %v must produce no verify change, got %d",
				engine, len(result.InPlaceChanges),
			)
		}
	}
}

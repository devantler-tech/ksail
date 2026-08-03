package diff_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

const fluxVerifyField = "cluster.workload.flux.verify"

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
	if change.Field != fluxVerifyField {
		t.Errorf("field = %q, want %q", change.Field, fluxVerifyField)
	}

	if change.Category != clusterupdate.ChangeCategoryInPlace {
		t.Errorf(
			"category = %q, want in-place — re-asserting verify must never demand recreation",
			change.Category,
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

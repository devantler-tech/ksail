package diff_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
)

func newVerifyEngine() *specdiff.Engine {
	return specdiff.NewEngine(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner)
}

// TestCheckFluxVerifyDetectsDrift is the core of platform#2922: spec.verify is
// applied by patching a resource the flux-operator generates, so it is not a
// field the structural diff walks. Without this change the configured policy
// produces no diff entry, cluster update runs no handler, and the infrastructure
// source stays unverified while the configuration reads as correct.
func TestCheckFluxVerifyDetectsDrift(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newVerifyEngine().CheckFluxVerify(true, v1alpha1.GitOpsEngineFlux, result)

	if len(result.InPlaceChanges) != 1 {
		t.Fatalf(
			"drifted verification must produce exactly one in-place change, got %d",
			len(result.InPlaceChanges),
		)
	}

	change := result.InPlaceChanges[0]
	if change.Field != specdiff.FluxVerifyField {
		t.Errorf("field = %q, want %q", change.Field, specdiff.FluxVerifyField)
	}

	if change.Category != clusterupdate.ChangeCategoryInPlace {
		t.Errorf(
			"category = %q, want in-place — re-asserting the FluxInstance never demands recreation",
			change.Category,
		)
	}
}

// TestCheckFluxVerifyIgnoresUndriftedState is the discriminating control: an
// engine that appended unconditionally would pass the test above and make every
// update report a phantom verification change.
func TestCheckFluxVerifyIgnoresUndriftedState(t *testing.T) {
	t.Parallel()

	result := clusterupdate.NewEmptyUpdateResult()
	newVerifyEngine().CheckFluxVerify(false, v1alpha1.GitOpsEngineFlux, result)

	if len(result.InPlaceChanges) != 0 {
		t.Errorf(
			"undrifted verification must produce no change, got %d",
			len(result.InPlaceChanges),
		)
	}
}

// TestCheckFluxVerifyIgnoresNonFluxEngines pins the engine guard. spec.verify is
// a Flux OCIRepository field; reporting it against ArgoCD would surface a change
// whose handler has nothing to patch.
func TestCheckFluxVerifyIgnoresNonFluxEngines(t *testing.T) {
	t.Parallel()

	for _, engine := range []v1alpha1.GitOpsEngine{
		v1alpha1.GitOpsEngineArgoCD,
		v1alpha1.GitOpsEngineNone,
	} {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			result := clusterupdate.NewEmptyUpdateResult()
			newVerifyEngine().CheckFluxVerify(true, engine, result)

			if len(result.InPlaceChanges) != 0 {
				t.Errorf(
					"engine %q must produce no verification change, got %d",
					engine, len(result.InPlaceChanges),
				)
			}
		})
	}
}
